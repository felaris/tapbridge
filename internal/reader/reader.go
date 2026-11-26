// Package reader watches a PC/SC contactless reader for card taps, reads the
// card's NDEF ID (falling back to its UID), and performs NDEF writes on
// request.
//
// It is written to survive the ways PC/SC misbehaves in practice, especially
// on Windows: the Smart Card service stopping when the last reader is
// unplugged, Smart Card Plug and Play briefly locking a fresh card, readers
// that expose a separate SAM slot, and driver panics.
package reader

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ebfe/scard"

	"github.com/felaris/tapbridge/internal/ndef"
)

// ErrWritePending is returned by RequestWrite when a write is already queued.
var ErrWritePending = errors.New("another write is pending")

// errCardGone means the card was removed or reset mid-operation.
var errCardGone = errors.New("card removed")

// Record types accepted by WriteRequest.
const (
	RecordText = "text"
	RecordURI  = "uri"
)

// WriteRequest asks for the next tapped card to be written.
type WriteRequest struct {
	ID         string
	RecordType string           // RecordText (default) or RecordURI
	Done       func(err *Error) // called once from the reader goroutine; nil on success
	// Canceled, if non-nil, is closed when the requester goes away (e.g. its
	// browser tab closed). A canceled request is dropped instead of being
	// written onto whatever card is tapped next.
	Canceled <-chan struct{}
}

func (r WriteRequest) canceled() bool {
	if r.Canceled == nil {
		return false
	}
	select {
	case <-r.Canceled:
		return true
	default:
		return false
	}
}

// Events are the callbacks a Service reports to. Nil callbacks are ignored.
// They are called from the reader goroutine and must not block for long.
type Events struct {
	Status  func(text string)
	Card    func(id string)
	Readers func(readers []string, active string) // only called on change
}

// Service polls PC/SC readers. Create one with New and start it with Run.
type Service struct {
	events   Events
	selected func() string // the user's preferred reader, "" for automatic

	mu      sync.Mutex
	pending *WriteRequest // at most one queued write
}

// New returns a Service. selected returns the reader name the user picked in
// the tray, or "" to choose automatically; it may be nil.
func New(events Events, selected func() string) *Service {
	if selected == nil {
		selected = func() string { return "" }
	}
	return &Service{events: events, selected: selected}
}

// RequestWrite queues req for the next tapped card. A pending request whose
// requester has gone away is replaced.
func (s *Service) RequestWrite(req WriteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending != nil && !s.pending.canceled() {
		return ErrWritePending
	}
	s.pending = &req
	return nil
}

// hasWrite reports whether a live write request is pending.
func (s *Service) hasWrite() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending != nil && !s.pending.canceled()
}

// takeWrite removes and returns the pending write request, discarding it if
// its requester has gone away.
func (s *Service) takeWrite() (WriteRequest, bool) {
	s.mu.Lock()
	p := s.pending
	s.pending = nil
	s.mu.Unlock()
	if p == nil {
		return WriteRequest{}, false
	}
	if p.canceled() {
		log.Printf("[nfc] dropped write for %q: the page that requested it disconnected", p.ID)
		return WriteRequest{}, false
	}
	return *p, true
}

func (s *Service) status(text string) {
	if s.events.Status != nil {
		s.events.Status(text)
	}
}

// Run polls forever, re-establishing the PC/SC context whenever it dies.
func (s *Service) Run() {
	told := false
	for {
		ctx, err := scard.EstablishContext()
		if err != nil {
			// Normal on Windows when no reader has been plugged in since boot:
			// the Smart Card service only starts when a reader appears.
			if !told {
				log.Printf("[nfc] PC/SC not available yet: %v", err)
				s.status("Waiting for reader... (plug in your NFC reader)")
				told = true
			}
			time.Sleep(2 * time.Second)
			continue
		}
		told = false
		err = s.poll(ctx)
		log.Printf("[nfc] PC/SC context lost (%v) — reconnecting", err)
		_ = ctx.Release()
		time.Sleep(time.Second)
	}
}

// poll runs until the PC/SC context dies, then returns so Run can establish a
// fresh one.
func (s *Service) poll(ctx *scard.Context) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic in poll loop: %v", r)
		}
	}()

	var (
		activeReader string
		lastUID      string
		menu         readerMenuState
		state        = scard.StateUnaware
		toldNoReader bool
	)
	menu.selected = "\x00" // force the first report

	for {
		readers, err := ctx.ListReaders()
		if err != nil && !errors.Is(err, scard.ErrNoReadersAvailable) {
			if isContextDead(err) {
				s.reportReaders(&menu, nil, "")
				if activeReader != "" {
					s.status("Reader removed — plug in reader")
				}
				return err
			}
			log.Printf("[nfc] list readers: %v", err)
		}

		selected := s.selected()
		s.reportReaders(&menu, readers, selected)

		if len(readers) == 0 {
			if activeReader != "" {
				activeReader, lastUID = "", ""
				toldNoReader = true
				s.status("Reader removed — plug in reader")
			} else if !toldNoReader {
				toldNoReader = true
				s.status("Waiting for reader... (plug in your NFC reader)")
			}
			time.Sleep(time.Second)
			continue
		}
		toldNoReader = false

		target := PickReader(readers, selected)
		if target != activeReader {
			activeReader, lastUID = target, ""
			state = scard.StateUnaware
			log.Printf("[nfc] using reader %q (available: %q)", target, readers)
			s.status("Reader ready — tap a card")
		}

		// Wait briefly for the reader's state to change instead of calling
		// SCardConnect in a tight loop. The timeout keeps us responsive to
		// hot-plug, tray selection changes and pending writes.
		rs := []scard.ReaderState{{Reader: activeReader, CurrentState: state}}
		err = ctx.GetStatusChange(rs, 300*time.Millisecond)
		changed := false
		switch {
		case err == nil:
			newState := rs[0].EventState &^ scard.StateChanged
			changed = cardChanged(state, newState)
			state = newState
		case errors.Is(err, scard.ErrTimeout):
			// no change
		case isContextDead(err):
			return err
		default:
			// Reader vanished or a driver hiccup — re-list readers next loop.
			state = scard.StateUnaware
			time.Sleep(300 * time.Millisecond)
			continue
		}

		if state&scard.StatePresent == 0 || state&scard.StateMute != 0 {
			if state&scard.StateEmpty != 0 {
				lastUID = ""
			}
			continue
		}

		pendingWrite := s.hasWrite()
		if lastUID != "" && !changed && !pendingWrite {
			continue // same card still on the reader, already reported
		}

		uid, err := s.handleCard(ctx, activeReader, lastUID)
		if err != nil {
			if isContextDead(err) {
				return err
			}
			if !errors.Is(err, errCardGone) {
				log.Printf("[nfc] %v", err)
				s.status(Explain(err).Message)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		lastUID = uid
	}
}

// readerMenuState remembers what was last reported through Events.Readers.
type readerMenuState struct {
	readers  []string
	selected string
}

// reportReaders calls Events.Readers only when the list or selection changed.
// Rewriting tray items on every poll makes the Windows tray menu flicker and
// close while the user has it open.
func (s *Service) reportReaders(m *readerMenuState, readers []string, selected string) {
	if selected == m.selected && equalStrings(readers, m.readers) {
		return
	}
	m.readers = append([]string(nil), readers...)
	m.selected = selected
	if s.events.Readers != nil {
		s.events.Readers(m.readers, PickReader(readers, selected))
	}
}

// handleCard connects to the present card, performs a pending write or a
// read, and returns the card's UID.
func (s *Service) handleCard(ctx *scard.Context, reader, lastUID string) (string, error) {
	card, err := connectCard(ctx, reader)
	if err != nil {
		return "", err
	}
	defer func() { _ = card.Disconnect(scard.LeaveCard) }()

	uidBytes, err := transmit(card, []byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
	if err != nil {
		return "", fmt.Errorf("get uid: %w", err)
	}
	uid := hex.EncodeToString(uidBytes)

	// A pending write request takes priority over reading.
	if req, ok := s.takeWrite(); ok {
		s.write(card, req)
		return uid, nil
	}

	if uid == lastUID {
		return uid, nil
	}

	id := ""
	if data := readNdef(card); len(data) > 0 {
		id = ndef.ParseID(data)
		log.Printf("[nfc] raw ndef (%d bytes): %s", len(data), hex.EncodeToString(data))
	}
	if id == "" {
		log.Printf("[nfc] no ndef parsed, using uid")
		id = uid
	}

	s.status("Card scanned: " + id)
	if s.events.Card != nil {
		s.events.Card(id)
	}
	return uid, nil
}

func (s *Service) write(card *scard.Card, req WriteRequest) {
	var data []byte
	var err error
	if req.RecordType == RecordURI {
		data, err = ndef.BuildURI(req.ID)
	} else {
		data, err = ndef.BuildText(req.ID)
	}
	if err == nil {
		err = writeNdef(card, data)
	}
	explained := Explain(err)
	if explained != nil {
		log.Printf("[nfc] write failed: %v (%s)", err, explained.Code)
		s.status("Write failed: " + explained.Message)
	} else {
		log.Printf("[nfc] write ok: %s", req.ID)
		s.status("Write ok: " + req.ID)
	}
	if req.Done != nil {
		req.Done(explained)
	}
}

// ── Reader selection ─────────────────────────────────────────────────────────

// isSAMReader reports whether a reader name is a Secure Access Module slot.
// Readers such as the ACR1252U / ACR1281U expose a contactless "PICC" slot
// and a "SAM" slot as separate PC/SC readers; on Windows the SAM slot is
// often listed first, and polling it never sees a tapped card.
func isSAMReader(name string) bool {
	u := strings.ToUpper(name)
	return strings.Contains(u, " SAM") || strings.HasPrefix(u, "SAM")
}

// PickReader chooses the reader to poll: the user's selection if it is still
// connected, otherwise the first contactless (PICC) reader, otherwise the
// first non-SAM reader, otherwise the first reader.
func PickReader(readers []string, selected string) string {
	if len(readers) == 0 {
		return ""
	}
	if selected != "" {
		for _, r := range readers {
			if r == selected {
				return selected
			}
		}
	}
	for _, r := range readers {
		u := strings.ToUpper(r)
		if strings.Contains(u, "PICC") || strings.Contains(u, "CONTACTLESS") {
			return r
		}
	}
	for _, r := range readers {
		if !isSAMReader(r) {
			return r
		}
	}
	return readers[0]
}

// cardChanged reports whether a reader state transition means a different
// card may now be present. Our own SCardConnect/Disconnect toggles the
// InUse/Exclusive bits, which must not count — otherwise we'd reconnect in a
// tight loop. The high 16 bits are the reader's card-insertion event counter
// on both Windows and pcsc-lite.
func cardChanged(old, new scard.StateFlag) bool {
	const presence = scard.StatePresent | scard.StateEmpty | scard.StateMute
	return old&presence != new&presence || old&0xFFFF0000 != new&0xFFFF0000
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── PC/SC helpers ────────────────────────────────────────────────────────────

// connectCard connects to the card on reader, retrying briefly: on Windows
// the Smart Card Plug and Play service grabs a freshly tapped card for a
// moment to look for a minidriver, causing transient sharing violations.
func connectCard(ctx *scard.Context, reader string) (*scard.Card, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		card, err := ctx.Connect(reader, scard.ShareShared, scard.ProtocolAny)
		if err == nil {
			return card, nil
		}
		lastErr = err
		if isContextDead(err) || errors.Is(err, scard.ErrNoSmartcard) || errors.Is(err, scard.ErrRemovedCard) {
			break
		}
		time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
	}
	if isCardGone(lastErr) {
		return nil, errCardGone
	}
	return nil, fmt.Errorf("connect %q: %w", reader, lastErr)
}

// transmit sends an APDU and returns the response body when the status word
// is 90 00. It never panics: ebfe/scard's Windows backend panics on a card
// whose active protocol is neither T=0 nor T=1, which would otherwise take
// the whole tray app down.
func transmit(card *scard.Card, cmd []byte) (body []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("transmit panicked: %v", r)
		}
	}()
	r, err := card.Transmit(cmd)
	if err != nil {
		if isCardGone(err) {
			return nil, errCardGone
		}
		return nil, err
	}
	if len(r) < 2 {
		return nil, fmt.Errorf("short response %X", r)
	}
	sw := r[len(r)-2:]
	if sw[0] != 0x90 || sw[1] != 0x00 {
		return nil, &statusError{sw1: sw[0], sw2: sw[1]}
	}
	return r[:len(r)-2], nil
}

func isCardGone(err error) bool {
	return errors.Is(err, scard.ErrRemovedCard) ||
		errors.Is(err, scard.ErrResetCard) ||
		errors.Is(err, scard.ErrNoSmartcard) ||
		errors.Is(err, scard.ErrUnpoweredCard) ||
		errors.Is(err, scard.ErrUnresponsiveCard)
}

// isContextDead reports whether the PC/SC context must be re-established.
// On Windows the Smart Card service stops itself when the last reader is
// unplugged, which invalidates every existing context.
func isContextDead(err error) bool {
	return errors.Is(err, scard.ErrServiceStopped) ||
		errors.Is(err, scard.ErrNoService) ||
		errors.Is(err, scard.ErrInvalidHandle) ||
		errors.Is(err, scard.ErrShutdown) ||
		errors.Is(err, scard.ErrSystemCancelled) ||
		errors.Is(err, scard.ErrCommError) ||
		errors.Is(err, scard.ErrInternalError) ||
		errors.Is(err, scard.ErrUnknownError)
}

// readPages reads 16 bytes (4 pages) starting at page — the most the ACR122U
// and most PC/SC drivers return for one READ BINARY.
func readPages(card *scard.Card, page byte) ([]byte, error) {
	b, err := transmit(card, []byte{0xFF, 0xB0, 0x00, page, 0x10})
	if err != nil {
		return nil, err
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("read page %d: short read (%d bytes)", page, len(b))
	}
	return b, nil
}

// dataAreaSize returns the tag's NDEF data area size in bytes from its
// capability container (page 3), or 0 if it can't be determined.
func dataAreaSize(card *scard.Card) int {
	hdr, err := readPages(card, 0)
	if err != nil || len(hdr) < 16 || hdr[12] != 0xE1 {
		return 0
	}
	return int(hdr[14]) * 8
}

// readNdef reads the Type 2 tag data area from page 4 until the NDEF TLV is
// complete, the capability-container size is reached, or a read fails.
func readNdef(card *scard.Card) []byte {
	maxData := dataAreaSize(card)
	if maxData == 0 {
		maxData = 48 // MIFARE Ultralight
	}
	if maxData > 1024 {
		maxData = 1024
	}

	var buf []byte
	for page := 4; len(buf) < maxData && page <= 0xFF; page += 4 {
		b, err := readPages(card, byte(page))
		if err != nil {
			if len(buf) == 0 {
				log.Printf("[nfc] read page %d failed: %v", page, err)
			}
			break
		}
		buf = append(buf, b...)
		if ndef.Complete(buf) {
			break
		}
	}
	if len(buf) > maxData {
		buf = buf[:maxData]
	}
	return buf
}

// writeNdef writes page-aligned NDEF data starting at page 4.
func writeNdef(card *scard.Card, data []byte) error {
	if len(data)%4 != 0 {
		return fmt.Errorf("ndef data not page-aligned (%d bytes)", len(data))
	}
	// Refuse to write past the data area, which would clobber lock/config pages.
	if capacity := dataAreaSize(card); capacity > 0 && len(data) > capacity {
		return &tagTooSmallError{need: len(data), have: capacity}
	}
	for i := 0; i < len(data); i += 4 {
		page := byte(4 + i/4)
		cmd := append([]byte{0xFF, 0xD6, 0x00, page, 0x04}, data[i:i+4]...)
		if _, err := transmit(card, cmd); err != nil {
			return fmt.Errorf("write page %d: %w", page, err)
		}
	}
	return nil
}
