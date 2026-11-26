package main

import (
	"net"
	"strings"
	"testing"

	"github.com/felaris/tapbridge/internal/config"
)

func TestPortInUseIsExplained(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	cfg := config.Default()
	cfg.Port = port
	err = listen(cfg, nil)
	if err == nil {
		t.Fatal("listening on a busy port succeeded")
	}
	if !strings.Contains(err.Error(), "already in use") || !strings.Contains(err.Error(), "--port") {
		t.Errorf("unhelpful error: %v", err)
	}
}
