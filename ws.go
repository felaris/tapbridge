package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type Msg struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	RecordType string `json:"recordType,omitempty"` // "text" (default) or "uri" — write requests only
	Message    string `json:"message,omitempty"`
}

type writeReq struct {
	id         string
	recordType string
	conn       *websocket.Conn
}

var (
	mu      sync.Mutex
	clients = make(map[*websocket.Conn]bool)

	writeCh = make(chan writeReq, 1)

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			ok := isAllowedOrigin(origin, getConfig().AllowedOrigins)
			if !ok {
				log.Printf("[tapbridge] rejected WebSocket connection from origin %q", origin)
			}
			return ok
		},
	}
)

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
			recordType := msg.RecordType
			if recordType != "uri" {
				recordType = "text"
			}
			select {
			case writeCh <- writeReq{id: msg.ID, recordType: recordType, conn: conn}:
				setStatus("Ready to write — tap card")
			default:
				_ = conn.WriteJSON(Msg{Type: "write_error", Message: "another write is pending"})
			}
		}
	}
}
