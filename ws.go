package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

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
	client     *wsClient
}

// wsClient wraps a connection with a write lock. gorilla/websocket allows
// only one concurrent writer per connection; the NFC goroutine (broadcasts,
// write results) and the connection's own handler both write, and an
// unsynchronised overlap panics and kills the app.
type wsClient struct {
	conn *websocket.Conn
	wmu  sync.Mutex
}

func (c *wsClient) send(m Msg) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.sendRaw(data)
}

func (c *wsClient) sendRaw(data []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = c.conn.WriteMessage(websocket.TextMessage, data)
}

var (
	mu      sync.Mutex
	clients = make(map[*wsClient]bool)

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
	list := make([]*wsClient, 0, len(clients))
	for c := range clients {
		list = append(list, c)
	}
	mu.Unlock()
	for _, c := range list {
		c.sendRaw(data)
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &wsClient{conn: conn}
	mu.Lock()
	clients[client] = true
	mu.Unlock()
	setStatus("Browser connected — tap a card")
	client.send(Msg{Type: "ready"})

	defer func() {
		mu.Lock()
		delete(clients, client)
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
			case writeCh <- writeReq{id: msg.ID, recordType: recordType, client: client}:
				setStatus("Ready to write — tap card")
			default:
				client.send(Msg{Type: "write_error", Message: "another write is pending"})
			}
		}
	}
}
