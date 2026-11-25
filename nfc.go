package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ebfe/scard"
)

const maxReaderSlots = 8

// ── APDU helpers ──────────────────────────────────────────────────────────────

// errCardGone means the card was removed or reset mid-operation.
var errCardGone = errors.New("card removed")

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
		return nil, fmt.Errorf("status %X", sw)
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
// On Windows the Smart Card service (SCardSvr) stops itself when the last
// reader is unplugged, which invalidates every existing context — without a
// fresh EstablishContext the app would never see the reader again.
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

// readPages reads 16 bytes (4 pages) starting at page. 16 bytes is the most
// the ACR122U and most PC/SC drivers return for a single READ BINARY.
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

// ── NDEF reading ──────────────────────────────────────────────────────────────

// readNdef reads the NFC Forum Type 2 data area (page 4 onward) until the
// TLV terminator, the capability-container size, or a read error — so long
// URIs written by this bridge read back in full instead of being cut at 48
// bytes.
func readNdef(card *scard.Card) []byte {
	// Pages 0–3 hold the UID, lock bytes and the capability container (CC).
	maxData := 48
	if hdr, err := readPages(card, 0); err == nil && len(hdr) >= 16 && hdr[12] == 0xE1 {
		if n := int(hdr[14]) * 8; n > 0 {
			maxData = n
		}
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
		if ndefComplete(buf) {
			break
		}
	}
	if len(buf) > maxData {
		buf = buf[:maxData]
	}
	return buf
}

// ── NDEF writing ──────────────────────────────────────────────────────────────

// writeNdef writes NDEF data to the card starting at page 4, 4 bytes per page.
func writeNdef(card *scard.Card, data []byte) error {
	if len(data)%4 != 0 {
		return fmt.Errorf("ndef data not page-aligned (%d bytes)", len(data))
	}
	// Refuse to write past the tag's data area (e.g. a 254-byte record onto a
	// 48-byte Ultralight would clobber lock/config pages).
	if hdr, err := readPages(card, 0); err == nil && len(hdr) >= 16 && hdr[12] == 0xE1 {
		if capacity := int(hdr[14]) * 8; capacity > 0 && len(data) > capacity {
			return fmt.Errorf("value too long for this tag (%d bytes, tag holds %d)", len(data), capacity)
		}
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

// ── Reader selection ──────────────────────────────────────────────────────────

// isSAMReader reports whether a reader name is a Secure Access Module slot.
// Readers such as the ACR1252U / ACR1281U expose a contactless "PICC" slot
// and a "SAM" slot as separate PC/SC readers; on Windows the SAM slot is
// frequently listed first, and polling it never sees a tapped card.
func isSAMReader(name string) bool {
	u := strings.ToUpper(name)
	return strings.Contains(u, " SAM") || strings.HasPrefix(u, "SAM")
}

// pickReader chooses the active reader: the user's configured selection if
// it's still connected, otherwise the first contactless (non-SAM) reader,
// otherwise the first reader.
func pickReader(readers []string, selected string) string {
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

// ── PC/SC NFC polling ─────────────────────────────────────────────────────────

func runNFC() {
	for {
		ctx, err := scard.EstablishContext()
		if err != nil {
			// On Windows this is normal when no reader has been plugged in
			// since boot: SCardSvr only starts when a reader appears.
			setStatus("Waiting for reader... (plug in your NFC reader)")
			time.Sleep(2 * time.Second)
			continue
		}
		err = poll(ctx)
		log.Printf("[nfc] PC/SC context lost (%v) — reconnecting", err)
		_ = ctx.Release()
		time.Sleep(1 * time.Second)
	}
}

// poll runs until the PC/SC context dies, then returns so runNFC can
// establish a fresh one.
func poll(ctx *scard.Context) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic in poll loop: %v", r)
		}
	}()

	var (
		activeReader string
		lastUID      string
		lastReaders  []string
		lastSelected = "\x00" // force first menu update
		state        = scard.StateUnaware
		toldNoReader bool
	)

	for {
		readers, err := ctx.ListReaders()
		if err != nil && !errors.Is(err, scard.ErrNoReadersAvailable) {
			if isContextDead(err) {
				updateReaderMenuIfChanged(&lastReaders, &lastSelected, nil, "")
				if activeReader != "" {
					setStatus("Reader removed — plug in reader")
				}
				return err
			}
			log.Printf("[nfc] list readers: %v", err)
		}

		selected := getConfig().SelectedReader
		updateReaderMenuIfChanged(&lastReaders, &lastSelected, readers, selected)

		if len(readers) == 0 {
			if activeReader != "" {
				activeReader, lastUID = "", ""
				toldNoReader = true
				setStatus("Reader removed — plug in reader")
			} else if !toldNoReader {
				toldNoReader = true
				setStatus("Waiting for reader... (plug in your NFC reader)")
			}
			time.Sleep(1 * time.Second)
			continue
		}

		target := pickReader(readers, selected)
		toldNoReader = false
		if target != activeReader {
			activeReader, lastUID = target, ""
			state = scard.StateUnaware
			log.Printf("[nfc] using reader %q (available: %q)", target, readers)
			setStatus("Reader ready — tap a card")
		}

		// Block (briefly) until the reader's state changes instead of hammering
		// SCardConnect every few hundred ms. The timeout keeps us responsive to
		// reader hot-plug, menu selection changes, and pending write requests.
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
			// Reader vanished or driver hiccup — re-list readers next loop.
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

		pendingWrite := len(writeCh) > 0
		if lastUID != "" && !changed && !pendingWrite {
			continue // same card still on the reader, already reported
		}

		uid, err := handleCard(ctx, activeReader, lastUID)
		if err != nil {
			if isContextDead(err) {
				return err
			}
			if !errors.Is(err, errCardGone) {
				log.Printf("[nfc] %v", err)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		lastUID = uid
	}
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

// handleCard connects to the present card, performs a pending write or a
// read, and returns the card's UID.
func handleCard(ctx *scard.Context, reader, lastUID string) (string, error) {
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

	// Write mode: a pending write request takes priority over reading.
	select {
	case req := <-writeCh:
		doWrite(card, req)
		return uid, nil
	default:
	}

	if uid == lastUID {
		return uid, nil
	}

	id := ""
	if ndefData := readNdef(card); len(ndefData) > 0 {
		id = parseNdef(ndefData)
		log.Printf("[nfc] raw ndef (%d bytes): %s", len(ndefData), hex.EncodeToString(ndefData))
	}
	if id == "" {
		log.Printf("[nfc] no ndef parsed, using uid")
		id = uid
	}

	setStatus("Card scanned: " + id)
	setLastScanned(id)
	broadcast(Msg{Type: "card", ID: id})
	return uid, nil
}

func doWrite(card *scard.Card, req writeReq) {
	var ndefData []byte
	var err error
	if req.recordType == "uri" {
		ndefData, err = buildUriNdef(req.id)
	} else {
		ndefData, err = buildTextNdef(req.id)
	}
	if err == nil {
		err = writeNdef(card, ndefData)
	}
	if err != nil {
		log.Printf("[nfc] write failed: %v", err)
		setStatus("Write failed — try again")
		req.client.send(Msg{Type: "write_error", Message: err.Error()})
		return
	}
	log.Printf("[nfc] write ok: %s", req.id)
	setStatus("Write ok: " + req.id)
	req.client.send(Msg{Type: "write_ok", ID: req.id})
}
