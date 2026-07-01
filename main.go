package main

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ebfe/scard"
	"github.com/getlantern/systray"
	"github.com/gorilla/websocket"
)

//go:embed icon.png
var iconData []byte

const wsPort = "8765"

var (
	mu       sync.Mutex
	clients  = make(map[*websocket.Conn]bool)
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	mStatus *systray.MenuItem
	writeCh = make(chan writeReq, 1)
)

type Msg struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

type writeReq struct {
	id   string
	conn *websocket.Conn
}

func broadcast(m Msg) {
	data, _ := json.Marshal(m)
	mu.Lock()
	defer mu.Unlock()
	for c := range clients {
		_ = c.WriteMessage(websocket.TextMessage, data)
	}
}

func setStatus(text string) {
	log.Printf("[bridge] %s", text)
	if mStatus != nil {
		mStatus.SetTitle(text)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	mu.Lock()
	clients[conn] = true
	mu.Unlock()
	setStatus("Browser connected — tap a card")
	_ = conn.WriteJSON(Msg{Type: "ready"})

	defer func() {
		mu.Lock()
		delete(clients, conn)
		n := len(clients)
		mu.Unlock()
		conn.Close()
		if n == 0 {
			setStatus("Waiting for browser...")
		}
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg Msg
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.Type == "write" && msg.ID != "" {
			select {
			case writeCh <- writeReq{id: msg.ID, conn: conn}:
				setStatus("Ready to write — tap card")
			default:
				_ = conn.WriteJSON(Msg{Type: "write_error", Message: "another write is pending"})
			}
		}
	}
}

// ── NDEF parsing ──────────────────────────────────────────────────────────────

func parseNdefRecord(msg []byte) string {
	if len(msg) < 3 {
		return ""
	}
	typeLen := int(msg[1])
	payLen := int(msg[2])
	if len(msg) < 3+typeLen+payLen {
		return ""
	}
	recType := string(msg[3 : 3+typeLen])
	payload := msg[3+typeLen : 3+typeLen+payLen]

	switch recType {
	case "U":
		if len(payload) < 1 {
			return ""
		}
		prefixes := []string{"", "http://www.", "https://www.", "http://", "https://"}
		prefix := ""
		if int(payload[0]) < len(prefixes) {
			prefix = prefixes[int(payload[0])]
		}
		url := prefix + string(payload[1:])
		if idx := strings.Index(url, "/verify/"); idx >= 0 {
			rest := url[idx+8:]
			if end := strings.IndexAny(rest, "/?#"); end >= 0 {
				return rest[:end]
			}
			return rest
		}
	case "T":
		if len(payload) < 1 {
			return ""
		}
		langLen := int(payload[0] & 0x3f)
		if 1+langLen >= len(payload) {
			return ""
		}
		return strings.TrimSpace(string(payload[1+langLen:]))
	}
	return ""
}

func parseNdef(data []byte) string {
	i := 0
	for i < len(data) {
		t := data[i]
		i++
		if t == 0xfe || i >= len(data) {
			break
		}
		if t == 0x00 {
			continue
		}
		l := int(data[i])
		i++
		if t == 0x03 && l > 0 && i+l <= len(data) {
			return parseNdefRecord(data[i : i+l])
		}
		i += l
	}
	return ""
}

// ── NDEF reading (tries bulk first, falls back to 16-byte chunks) ────────────

func readNdef(card *scard.Card) []byte {
	// Attempt 1: read all 48 bytes at once (pages 4–15)
	r, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, 0x04, 0x30})
	if err == nil && len(r) >= 2 && r[len(r)-2] == 0x90 && r[len(r)-1] == 0x00 && len(r) > 2 {
		log.Printf("[nfc] bulk read ok (%d bytes)", len(r)-2)
		return r[:len(r)-2]
	}
	log.Printf("[nfc] bulk read failed (sw=%X), trying chunks", r)

	// Attempt 2: read 16 bytes at a time (one native MIFARE READ per call)
	var buf []byte
	for page := byte(4); page <= 12; page += 4 {
		r, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, page, 0x10})
		if err != nil || len(r) < 2 || r[len(r)-2] != 0x90 {
			log.Printf("[nfc] chunk read page %d failed", page)
			break
		}
		buf = append(buf, r[:len(r)-2]...)
	}
	return buf
}

// ── NDEF writing ──────────────────────────────────────────────────────────────

// buildTextNdef encodes text as a well-known Text NDEF record wrapped in TLV.
func buildTextNdef(text string) []byte {
	lang := "en"
	payload := make([]byte, 0, 1+len(lang)+len(text))
	payload = append(payload, byte(len(lang)))
	payload = append(payload, []byte(lang)...)
	payload = append(payload, []byte(text)...)

	// MB=1 ME=1 SR=1 TNF=0x01 (Well-Known) | type_len=1 | payload_len | 'T' | payload
	record := []byte{0xD1, 0x01, byte(len(payload)), 'T'}
	record = append(record, payload...)

	// TLV: tag 0x03 | length | NDEF message | 0xFE terminator
	msg := []byte{0x03, byte(len(record))}
	msg = append(msg, record...)
	msg = append(msg, 0xFE)

	// Pad to 4-byte page boundary
	for len(msg)%4 != 0 {
		msg = append(msg, 0x00)
	}
	return msg
}

// writeNdef writes NDEF data to the card starting at page 4, 4 bytes per page.
func writeNdef(card *scard.Card, data []byte) error {
	for i := 0; i < len(data); i += 4 {
		page := byte(4 + i/4)
		chunk := data[i : i+4]
		cmd := append([]byte{0xFF, 0xD6, 0x00, page, 0x04}, chunk...)
		r, err := card.Transmit(cmd)
		if err != nil {
			return fmt.Errorf("write page %d: %w", page, err)
		}
		if len(r) < 2 || r[len(r)-2] != 0x90 {
			return fmt.Errorf("write page %d: status %X", page, r)
		}
	}
	return nil
}

// ── PC/SC NFC polling ─────────────────────────────────────────────────────────

func runNFC() {
	for {
		ctx, err := scard.EstablishContext()
		if err != nil {
			setStatus("PC/SC unavailable — retrying...")
			time.Sleep(3 * time.Second)
			continue
		}
		poll(ctx)
		ctx.Release()
		time.Sleep(2 * time.Second)
	}
}

func poll(ctx *scard.Context) {
	var activeReader string
	lastUID := ""

	for {
		readers, err := ctx.ListReaders()
		if err != nil || len(readers) == 0 {
			if activeReader != "" {
				activeReader = ""
				lastUID = ""
				setStatus("Reader removed — plug in reader")
			}
			time.Sleep(2 * time.Second)
			continue
		}

		if readers[0] != activeReader {
			activeReader = readers[0]
			lastUID = ""
			setStatus("Reader ready — tap a card")
		}

		card, err := ctx.Connect(activeReader, scard.ShareShared, scard.ProtocolAny)
		if err != nil {
			if lastUID != "" {
				lastUID = ""
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		resp, err := card.Transmit([]byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
		if err != nil || len(resp) < 2 || resp[len(resp)-2] != 0x90 {
			_ = card.Disconnect(scard.LeaveCard)
			lastUID = ""
			time.Sleep(500 * time.Millisecond)
			continue
		}

		uid := hex.EncodeToString(resp[:len(resp)-2])

		// Write mode: check for a pending write request before doing a read
		select {
		case req := <-writeCh:
			ndefData := buildTextNdef(req.id)
			if err := writeNdef(card, ndefData); err != nil {
				log.Printf("[nfc] write failed: %v", err)
				setStatus("Write failed — try again")
				_ = req.conn.WriteJSON(Msg{Type: "write_error", Message: err.Error()})
			} else {
				log.Printf("[nfc] write ok: %s", req.id)
				setStatus("Write ok: " + req.id)
				_ = req.conn.WriteJSON(Msg{Type: "write_ok", ID: req.id})
			}
			lastUID = uid
			_ = card.Disconnect(scard.LeaveCard)
			time.Sleep(500 * time.Millisecond)
			continue
		default:
		}

		// Read mode
		if uid == lastUID {
			_ = card.Disconnect(scard.LeaveCard)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastUID = uid

		id := ""
		ndefData := readNdef(card)
		if len(ndefData) > 0 {
			id = parseNdef(ndefData)
			log.Printf("[nfc] raw ndef (%d bytes): %s", len(ndefData), hex.EncodeToString(ndefData))
		}
		if id == "" {
			log.Printf("[nfc] no ndef parsed, using uid")
			id = uid
		}

		setStatus("Card scanned: " + id)
		broadcast(Msg{Type: "card", ID: id})

		_ = card.Disconnect(scard.LeaveCard)
		time.Sleep(500 * time.Millisecond)
	}
}

// ── System tray + entry point ─────────────────────────────────────────────────

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTooltip("Felaris NFC Bridge")

	mStatus = systray.AddMenuItem("Starting...", "")
	mStatus.Disable()
	systray.AddSeparator()
	mPort := systray.AddMenuItem("ws://localhost:"+wsPort, "")
	mPort.Disable()
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit NFC Bridge", "")

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()

	go runNFC()
	go func() {
		http.HandleFunc("/", wsHandler)
		setStatus("Waiting for reader...")
		if err := http.ListenAndServe(":"+wsPort, nil); err != nil {
			log.Fatalf("WebSocket server: %v", err)
		}
	}()
}

func main() {
	log.SetFlags(log.Ltime)
	systray.Run(onReady, func() {})
}
