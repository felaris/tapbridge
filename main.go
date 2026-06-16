package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ebfe/scard"
	"github.com/gorilla/websocket"
)

const wsPort = "8765"

var (
	mu       sync.Mutex
	clients  = make(map[*websocket.Conn]bool)
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
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

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	mu.Lock()
	clients[conn] = true
	n := len(clients)
	mu.Unlock()
	log.Printf("[ws] browser connected (%d active)", n)
	_ = conn.WriteJSON(Msg{Type: "ready"})

	defer func() {
		mu.Lock()
		delete(clients, conn)
		n = len(clients)
		mu.Unlock()
		conn.Close()
		log.Printf("[ws] browser disconnected (%d active)", n)
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
			log.Printf("[nfc] PC/SC unavailable: %v — retry in 3s", err)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("[nfc] PC/SC ready")
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
				log.Println("[nfc] reader removed, waiting...")
				activeReader = ""
				lastUID = ""
			}
			time.Sleep(2 * time.Second)
			continue
		}

		if readers[0] != activeReader {
			activeReader = readers[0]
			lastUID = ""
			log.Printf("[nfc] reader ready: %s", activeReader)
		}

		card, err := ctx.Connect(activeReader, scard.ShareShared, scard.ProtocolAny)
		if err != nil {
			if lastUID != "" {
				lastUID = "" // card removed
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// FF CA 00 00 00 — Get UID
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

		// FF B0 00 04 30 — Read NDEF (pages 4–15, 48 bytes)
		id := ""
		ndefResp, err := card.Transmit([]byte{0xFF, 0xB0, 0x00, 0x04, 0x30})
		if err == nil && len(ndefResp) >= 2 && ndefResp[len(ndefResp)-2] == 0x90 {
			id = parseNdef(ndefResp[:len(ndefResp)-2])
		}
		if id == "" {
			id = uid
		}

		log.Printf("[nfc] card → %s", id)
		broadcast(Msg{Type: "card", ID: id})

		_ = card.Disconnect(scard.LeaveCard)
		time.Sleep(500 * time.Millisecond)
	}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	log.SetFlags(log.Ltime)
	log.Printf("Felaris NFC Bridge  —  ws://localhost:%s", wsPort)
	log.Println("Plug in your ACR122U and tap a card...")

	go runNFC()

	http.HandleFunc("/", wsHandler)
	log.Printf("Listening on :%s", wsPort)
	if err := http.ListenAndServe(":"+wsPort, nil); err != nil {
		log.Fatalf("Server: %v", err)
	}
}
