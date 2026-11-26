// Package server implements TapBridge's local WebSocket protocol.
//
// Every connected client receives a "ready" message on connect and a "card"
// message for every tap. A client may send a "write" request; the result is
// reported back to that client only as "write_ok" or "write_error".
// See docs/protocol.md for the full protocol.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message types.
const (
	TypeReady      = "ready"
	TypeCard       = "card"
	TypeWrite      = "write"
	TypeWriteOK    = "write_ok"
	TypeWriteError = "write_error"
	TypeError      = "error"
)

// Error codes produced by the server itself. Reader errors carry their own
// codes (see the reader package and docs/protocol.md).
const (
	CodeOriginNotAllowed = "origin_not_allowed"
	CodeInvalidRequest   = "invalid_request"
	CodeNotSupported     = "not_supported"
	CodeUnknown          = "unknown"
)

// CodedError is implemented by errors that carry a protocol error code.
type CodedError interface {
	error
	ErrorCode() string
}

// Msg is the JSON envelope for every message in both directions.
type Msg struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	RecordType string `json:"recordType,omitempty"` // "text" (default) or "uri" — write requests only
	Code       string `json:"code,omitempty"`       // machine-readable error code (error messages only)
	Message    string `json:"message,omitempty"`    // human-readable explanation (error messages only)
}

const (
	writeTimeout = 5 * time.Second
	maxMsgSize   = 4 << 10 // requests are tiny; cap what a client can make us buffer
)

// Options configures a Server. All callbacks are optional.
type Options struct {
	// AllowedOrigins returns the current browser-origin allowlist. It is
	// consulted on every connection attempt.
	AllowedOrigins func() []string
	// IsAllowedOrigin decides whether origin may connect given the allowlist.
	IsAllowedOrigin func(origin string, allowed []string) bool
	// OnWrite is called for a client's write request. reply delivers the
	// result to that client. A non-nil error is sent back immediately as
	// write_error (e.g. when a write is already pending).
	// gone is closed when the requesting client disconnects.
	OnWrite func(id, recordType string, reply func(Msg), gone <-chan struct{}) error
	// OnClients is called with the number of connected clients whenever it
	// changes.
	OnClients func(n int)
}

// Server is an http.Handler serving the WebSocket endpoint.
type Server struct {
	opts     Options
	upgrader websocket.Upgrader

	mu      sync.Mutex
	clients map[*client]struct{}
}

// New returns a Server.
func New(opts Options) *Server {
	s := &Server{opts: opts, clients: make(map[*client]struct{})}
	// Origins are checked after the upgrade (see ServeHTTP) so a rejected
	// page gets a readable reason instead of an opaque "connection failed".
	s.upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return s
}

func (s *Server) originAllowed(origin string) bool {
	if s.opts.IsAllowedOrigin == nil {
		return true
	}
	var allowed []string
	if s.opts.AllowedOrigins != nil {
		allowed = s.opts.AllowedOrigins()
	}
	return s.opts.IsAllowedOrigin(origin, allowed)
}

// reject tells a disallowed page why, then closes the connection. The client
// is never registered, so it receives no card data.
func reject(conn *websocket.Conn, origin string) {
	log.Printf("[ws] rejected connection from origin %q — allow it with --allow-origin %s", origin, origin)
	msg := fmt.Sprintf("TapBridge refused this page because its origin (%s) is not on the allowlist. "+
		"Restart TapBridge with --allow-origin %s, or set TAPBRIDGE_ALLOWED_ORIGINS. See docs/configuration.md.", origin, origin)
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = conn.WriteJSON(Msg{Type: TypeError, Code: CodeOriginNotAllowed, Message: msg})
	reason := "origin not allowed: " + origin
	if len(reason) > 120 { // close reasons are limited to 123 bytes
		reason = reason[:120]
	}
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason), time.Now().Add(time.Second))
	_ = conn.Close()
}

// client wraps a connection with a write lock: gorilla/websocket supports
// only one concurrent writer, and both the reader goroutine (broadcasts,
// write results) and the connection's handler write to it.
type client struct {
	conn *websocket.Conn
	wmu  sync.Mutex
	gone chan struct{} // closed on disconnect
}

func (c *client) send(m Msg) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.sendRaw(data)
}

func (c *client) sendRaw(data []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = c.conn.WriteMessage(websocket.TextMessage, data)
}

// Broadcast sends m to every connected client.
func (s *Server) Broadcast(m Msg) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	s.mu.Lock()
	list := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		list = append(list, c)
	}
	s.mu.Unlock()
	for _, c := range list {
		c.sendRaw(data)
	}
}

// Clients returns the number of connected clients.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *Server) add(c *client) {
	s.mu.Lock()
	s.clients[c] = struct{}{}
	n := len(s.clients)
	s.mu.Unlock()
	if s.opts.OnClients != nil {
		s.opts.OnClients(n)
	}
}

func (s *Server) remove(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	n := len(s.clients)
	s.mu.Unlock()
	if s.opts.OnClients != nil {
		s.opts.OnClients(n)
	}
}

// ServeHTTP upgrades WebSocket requests. Plain HTTP requests get a short
// plain-text reply, which is handy for checking that TapBridge is running.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "TapBridge is running. Connect with a WebSocket client.")
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader has already written an error response
	}
	if origin := r.Header.Get("Origin"); !s.originAllowed(origin) {
		reject(conn, origin)
		return
	}
	conn.SetReadLimit(maxMsgSize)

	c := &client{conn: conn, gone: make(chan struct{})}
	s.add(c)
	defer func() {
		close(c.gone)
		s.remove(c)
		_ = conn.Close()
	}()

	c.send(Msg{Type: TypeReady})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg Msg
		if json.Unmarshal(raw, &msg) != nil {
			c.send(Msg{Type: TypeError, Code: CodeInvalidRequest,
				Message: "Couldn't understand that message: it must be JSON like {\"type\":\"write\",\"id\":\"abc\"}."})
			continue
		}
		switch msg.Type {
		case TypeWrite:
			s.handleWrite(c, msg)
		default:
			c.send(Msg{Type: TypeError, Code: CodeInvalidRequest,
				Message: fmt.Sprintf("Unknown message type %q. The only message clients can send is \"write\".", msg.Type)})
		}
	}
}

func (s *Server) handleWrite(c *client, msg Msg) {
	if msg.ID == "" {
		c.send(Msg{Type: TypeWriteError, Code: CodeInvalidRequest,
			Message: "The write request has no \"id\". Send the value to write, e.g. {\"type\":\"write\",\"id\":\"abc123\"}."})
		return
	}
	recordType := "text"
	if msg.RecordType == "uri" {
		recordType = "uri"
	}
	if s.opts.OnWrite == nil {
		c.send(Msg{Type: TypeWriteError, Code: CodeNotSupported, Message: "Writing is not supported by this TapBridge."})
		return
	}
	if err := s.opts.OnWrite(msg.ID, recordType, c.send, c.gone); err != nil {
		c.send(ErrorMsg(TypeWriteError, err))
	}
}

// ErrorMsg builds an error message of the given type from err, using its
// code when it implements CodedError.
func ErrorMsg(typ string, err error) Msg {
	code := CodeUnknown
	if ce, ok := err.(CodedError); ok {
		code = ce.ErrorCode()
	}
	return Msg{Type: typ, Code: code, Message: err.Error()}
}
