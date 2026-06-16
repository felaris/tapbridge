package main

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
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
)

type Msg struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
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
	n := len(clients)
	mu.Unlock()
	setStatus("Browser connected — tap a card")
	_ = conn.WriteJSON(Msg{Type: "ready"})

	defer func() {
		mu.Lock()
		delete(clients, conn)
		n = len(clients)
		mu.Unlock()
		conn.Close()
		if n == 0 {
			setStatus("Waiting for browser...")
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
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
		text := strings.TrimSpace(string(payload[1+langLen:]))
		if d := onlyDigits(text); len(d) >= 6 {
			return d
		}
		return text
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

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
		if uid == lastUID {
			_ = card.Disconnect(scard.LeaveCard)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastUID = uid

		id := ""
		ndefResp, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, 0x04, 0x30})
		if err == nil && len(ndefResp) >= 2 && ndefResp[len(ndefResp)-2] == 0x90 {
			id = parseNdef(ndefResp[:len(ndefResp)-2])
		}
		if id == "" {
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
