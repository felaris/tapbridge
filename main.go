package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/getlantern/systray"
)

func onReady(cfg Config) {
	buildTray(cfg)

	go runNFC()
	go func() {
		addr := net.JoinHostPort(cfg.Host, cfg.Port)
		if !isLoopbackHost(cfg.Host) {
			log.Printf("[tapbridge] WARNING: binding to %s exposes card data beyond localhost", addr)
		}
		// Bind the port up front so a conflict (another instance, or another
		// app already on this port) surfaces in the tray instead of silently
		// killing the process.
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			setStatus("Port " + cfg.Port + " in use — is TapBridge already running?")
			log.Printf("[tapbridge] cannot bind %s: %v", addr, err)
			return
		}
		log.Printf("[tapbridge] listening on ws://%s", addr)

		handler := http.HandlerFunc(wsHandler)

		// On Windows "localhost" resolves to ::1 first, and some clients
		// (Node 17+, .NET, some WebView hosts) never fall back to 127.0.0.1.
		// When bound to IPv4 loopback, also listen on IPv6 loopback so
		// ws://localhost works everywhere. Best effort: IPv6 may be disabled.
		if cfg.Host == "127.0.0.1" || cfg.Host == "localhost" {
			if ln6, err := net.Listen("tcp", net.JoinHostPort("::1", cfg.Port)); err == nil {
				log.Printf("[tapbridge] also listening on ws://[::1]:%s", cfg.Port)
				go func() { _ = http.Serve(ln6, handler) }()
			}
		}

		if err := http.Serve(ln, handler); err != nil {
			log.Printf("[tapbridge] WebSocket server stopped: %v", err)
		}
	}()
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	flag.Parse()
	setupLogging()

	cfg := loadConfig()
	appConfigMu.Lock()
	appConfig = cfg
	appConfigMu.Unlock()

	systray.Run(func() { onReady(cfg) }, func() {})
}
