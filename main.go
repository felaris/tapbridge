package main

import (
	_ "embed"
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.png
var iconData []byte

func onReady(cfg Config) {
	buildTray(cfg)

	go runNFC()
	go func() {
		// Bind the port up front so a conflict (another instance, or another
		// app already on this port) surfaces in the tray instead of silently
		// killing the process.
		ln, err := net.Listen("tcp", ":"+cfg.Port)
		if err != nil {
			setStatus("Port " + cfg.Port + " in use — is TapBridge already running?")
			log.Printf("[tapbridge] cannot bind port %s: %v", cfg.Port, err)
			return
		}
		setStatus("Waiting for reader...")
		if err := http.Serve(ln, http.HandlerFunc(wsHandler)); err != nil {
			log.Printf("[tapbridge] WebSocket server stopped: %v", err)
		}
	}()
}

func main() {
	log.SetFlags(log.Ltime)
	flag.Parse()

	cfg := loadConfig()
	appConfigMu.Lock()
	appConfig = cfg
	appConfigMu.Unlock()

	systray.Run(func() { onReady(cfg) }, func() {})
}
