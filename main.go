package main

import (
	_ "embed"
	"flag"
	"log"
	"net/http"

	"github.com/getlantern/systray"
)

//go:embed assets/icon.png
var iconData []byte

func onReady(cfg Config) {
	buildTray(cfg)

	go runNFC()
	go func() {
		setStatus("Waiting for reader...")
		if err := http.ListenAndServe(":"+cfg.Port, http.HandlerFunc(wsHandler)); err != nil {
			log.Fatalf("WebSocket server: %v", err)
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
