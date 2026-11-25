package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

var logPath string

// setupLogging sends log output to a file in the config directory as well as
// stderr. The Windows release is built as a GUI app (no console window), so
// without a log file there is no way to see why a reader isn't working.
func setupLogging() {
	cfgFile, err := configPath()
	if err != nil {
		return
	}
	dir := filepath.Dir(cfgFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	p := filepath.Join(dir, "tapbridge.log")
	// Keep the log from growing without bound: start fresh once it passes 1 MB.
	if st, err := os.Stat(p); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logPath = p
	// File first: in a GUI-subsystem build stderr is invalid, and MultiWriter
	// stops at the first writer that errors.
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.Printf("[tapbridge] starting (%s/%s), log: %s", runtime.GOOS, runtime.GOARCH, p)
}

func logFilePath() string { return logPath }

func openPath(p string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	default:
		return exec.Command("xdg-open", p).Start()
	}
}
