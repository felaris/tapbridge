// Package logging writes TapBridge's log to a file next to config.json as
// well as stderr. The Windows release is a GUI-subsystem app with no console,
// so the log file is the only way to see what it is doing.
package logging

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const maxSize = 1 << 20 // rotate to tapbridge.log.old past 1 MB

// Setup starts logging to dir/tapbridge.log and returns the file's path, or
// "" if the file could not be opened (logging then goes to stderr only).
func Setup(dir string) string {
	log.SetFlags(log.Ldate | log.Ltime)
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return ""
	}
	path := filepath.Join(dir, "tapbridge.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return ""
	}
	// File first: in a GUI-subsystem build stderr is invalid, and
	// io.MultiWriter stops at the first writer that fails.
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	return path
}

// Open shows a file in the platform's default viewer.
func Open(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
