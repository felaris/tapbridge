package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// logUI is the headless UI: status goes to the log.
type logUI struct{}

func (logUI) SetStatus(text string) { log.Printf("[tapbridge] %s", text) }
func (logUI) SetLastScanned(string) {}
func (logUI) SetReaders(readers []string, active string) {
	if len(readers) > 0 {
		log.Printf("[tapbridge] readers: %s (using %q)", strings.Join(readers, ", "), active)
	}
}

// runHeadless runs until SIGINT/SIGTERM (e.g. `docker stop`).
func runHeadless(a *app) {
	if !a.start(logUI{}) {
		os.Exit(1)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	log.Printf("[tapbridge] received %v, shutting down", s)
}
