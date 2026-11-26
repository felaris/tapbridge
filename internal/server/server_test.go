package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func allowOnly(allowed string) Options {
	return Options{
		AllowedOrigins:  func() []string { return []string{allowed} },
		IsAllowedOrigin: func(o string, a []string) bool { return o == "" || o == a[0] },
	}
}

func dial(t *testing.T, srv *httptest.Server, origin string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), h)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func read(t *testing.T, c *websocket.Conn) Msg {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var m Msg
	if err := c.ReadJSON(&m); err != nil {
		t.Fatalf("read: %v", err)
	}
	return m
}

func TestReadyAndBroadcast(t *testing.T) {
	s := New(Options{})
	srv := httptest.NewServer(s)
	defer srv.Close()

	a, b := dial(t, srv, ""), dial(t, srv, "")
	for _, c := range []*websocket.Conn{a, b} {
		if m := read(t, c); m.Type != TypeReady {
			t.Fatalf("first message = %+v, want ready", m)
		}
	}
	s.Broadcast(Msg{Type: TypeCard, ID: "04a1b2"})
	for _, c := range []*websocket.Conn{a, b} {
		if m := read(t, c); m.Type != TypeCard || m.ID != "04a1b2" {
			t.Fatalf("broadcast = %+v", m)
		}
	}
}

func TestOriginRejectedWithReadableReason(t *testing.T) {
	s := New(allowOnly("http://localhost:3000"))
	srv := httptest.NewServer(s)
	defer srv.Close()

	ok := dial(t, srv, "http://localhost:3000")
	if m := read(t, ok); m.Type != TypeReady {
		t.Fatalf("allowed origin got %+v", m)
	}

	bad := dial(t, srv, "https://evil.example")
	m := read(t, bad)
	if m.Type != TypeError || m.Code != CodeOriginNotAllowed || !strings.Contains(m.Message, "--allow-origin https://evil.example") {
		t.Fatalf("rejection = %+v", m)
	}
	_ = bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := bad.ReadMessage()
	if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("want policy-violation close, got %v", err)
	}
	s.Broadcast(Msg{Type: TypeCard, ID: "secret"})
	if n := s.Clients(); n != 1 {
		t.Errorf("rejected client must not be registered (clients=%d)", n)
	}
}

func TestWriteFlow(t *testing.T) {
	var (
		mu      sync.Mutex
		gotID   string
		gotType string
		reply   func(Msg)
	)
	opts := Options{OnWrite: func(id, rt string, r func(Msg), _ <-chan struct{}) error {
		mu.Lock()
		defer mu.Unlock()
		if reply != nil {
			return errors.New("another write is pending")
		}
		gotID, gotType, reply = id, rt, r
		return nil
	}}
	srv := httptest.NewServer(New(opts))
	defer srv.Close()
	c := dial(t, srv, "")
	read(t, c) // ready

	_ = c.WriteJSON(Msg{Type: TypeWrite, ID: "https://x.test/verify/1", RecordType: "uri"})
	_ = c.WriteJSON(Msg{Type: TypeWrite, ID: "second"})
	if m := read(t, c); m.Type != TypeWriteError || m.Code != CodeUnknown || !strings.Contains(m.Message, "pending") {
		t.Fatalf("second write = %+v, want pending error", m)
	}

	mu.Lock()
	if gotID != "https://x.test/verify/1" || gotType != "uri" {
		t.Errorf("OnWrite got (%q, %q)", gotID, gotType)
	}
	r := reply
	mu.Unlock()
	r(Msg{Type: TypeWriteOK, ID: gotID})
	if m := read(t, c); m.Type != TypeWriteOK {
		t.Fatalf("result = %+v, want write_ok", m)
	}
}

func TestWriteValidation(t *testing.T) {
	srv := httptest.NewServer(New(Options{}))
	defer srv.Close()
	c := dial(t, srv, "")
	read(t, c)

	_ = c.WriteJSON(Msg{Type: TypeWrite})
	if m := read(t, c); m.Type != TypeWriteError {
		t.Fatalf("empty id = %+v, want write_error", m)
	}
	_ = c.WriteMessage(websocket.TextMessage, []byte("not json"))
	if m := read(t, c); m.Type != TypeError || m.Code != CodeInvalidRequest {
		t.Fatalf("bad json = %+v", m)
	}
	_ = c.WriteJSON(Msg{Type: "bogus"})
	if m := read(t, c); m.Type != TypeError || !strings.Contains(m.Message, "bogus") {
		t.Fatalf("unknown type = %+v", m)
	}
	_ = c.WriteJSON(Msg{Type: TypeWrite, ID: "x"})
	if m := read(t, c); m.Type != TypeWriteError || !strings.Contains(m.Message, "not supported") {
		t.Fatalf("no OnWrite = %+v", m)
	}
}

// Broadcasting while a client's own write result is being sent must not
// race (gorilla/websocket panics on concurrent writers). Run with -race.
func TestConcurrentSends(t *testing.T) {
	var reply func(Msg)
	ready := make(chan struct{})
	s := New(Options{OnWrite: func(_, _ string, r func(Msg), _ <-chan struct{}) error {
		reply = r
		close(ready)
		return nil
	}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	c := dial(t, srv, "")
	read(t, c)
	_ = c.WriteJSON(Msg{Type: TypeWrite, ID: "x"})
	<-ready

	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Broadcast(Msg{Type: TypeCard, ID: "a"}) }()
		go func() { defer wg.Done(); reply(Msg{Type: TypeWriteOK, ID: "x"}) }()
	}
	wg.Wait()
}

func TestClientCountAndPlainHTTP(t *testing.T) {
	counts := make(chan int, 4)
	s := New(Options{OnClients: func(n int) { counts <- n }})
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "TapBridge is running") {
		t.Errorf("plain HTTP body = %q", body)
	}

	c := dial(t, srv, "")
	if n := <-counts; n != 1 {
		t.Errorf("after connect n = %d", n)
	}
	c.Close()
	select {
	case n := <-counts:
		if n != 0 {
			t.Errorf("after close n = %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Error("disconnect not reported")
	}
}

func TestWriteCanceledOnDisconnect(t *testing.T) {
	goneCh := make(chan (<-chan struct{}), 1)
	srv := httptest.NewServer(New(Options{OnWrite: func(_, _ string, _ func(Msg), gone <-chan struct{}) error {
		goneCh <- gone
		return nil
	}}))
	defer srv.Close()
	c := dial(t, srv, "")
	read(t, c)
	_ = c.WriteJSON(Msg{Type: TypeWrite, ID: "x"})
	gone := <-goneCh
	c.Close()
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("gone channel not closed after the client disconnected")
	}
}
