// Package config resolves TapBridge's settings and persists the ones the user
// changes from the tray menu.
//
// Settings are layered in increasing priority:
// built-in defaults → config file → environment variables → CLI flags.
package config

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Environment variable names.
const (
	EnvHost    = "TAPBRIDGE_HOST"
	EnvPort    = "TAPBRIDGE_PORT"
	EnvOrigins = "TAPBRIDGE_ALLOWED_ORIGINS"
)

// Config holds runtime settings.
type Config struct {
	Host           string   `json:"host,omitempty"`
	Port           string   `json:"port,omitempty"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	SelectedReader string   `json:"selectedReader,omitempty"`
	AutoStart      bool     `json:"autoStart"`
}

// Overrides are values supplied on the command line. Empty fields mean
// "not set".
type Overrides struct {
	Host    string
	Port    string
	Origins string // comma-separated
}

// Default returns the built-in settings.
func Default() Config {
	return Config{
		// Loopback only: the WebSocket exposes card IDs and accepts no-Origin
		// (non-browser) clients, so it must not be reachable from the LAN
		// unless the operator opts in explicitly.
		Host: "127.0.0.1",
		Port: "8765",
		AllowedOrigins: []string{
			"http://localhost",
			"https://localhost",
			"http://127.0.0.1",
			"https://127.0.0.1",
		},
	}
}

// Dir returns the per-user directory holding config.json and tapbridge.log:
// ~/Library/Application Support/tapbridge on macOS, %AppData%\tapbridge on
// Windows, and $XDG_CONFIG_HOME/tapbridge elsewhere.
func Dir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tapbridge"), nil
}

// Path returns the location of config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file (if any) and applies environment variable and
// command-line overrides on top.
func Load(o Overrides) Config {
	cfg := Default()
	if path, err := Path(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			cfg = merge(cfg, data)
		}
	}
	return applyOverrides(cfg, o, os.Getenv)
}

func merge(cfg Config, data []byte) Config {
	var file Config
	if json.Unmarshal(data, &file) != nil {
		return cfg
	}
	if file.Host != "" {
		cfg.Host = file.Host
	}
	if file.Port != "" {
		cfg.Port = file.Port
	}
	if len(file.AllowedOrigins) > 0 {
		cfg.AllowedOrigins = file.AllowedOrigins
	}
	cfg.SelectedReader = file.SelectedReader
	cfg.AutoStart = file.AutoStart
	return cfg
}

func applyOverrides(cfg Config, o Overrides, getenv func(string) string) Config {
	pick := func(flag, env string) string {
		if flag != "" {
			return flag
		}
		return getenv(env)
	}
	if v := pick(o.Host, EnvHost); v != "" {
		cfg.Host = v
	}
	if v := pick(o.Port, EnvPort); v != "" {
		cfg.Port = v
	}
	if v := pick(o.Origins, EnvOrigins); v != "" {
		cfg.AllowedOrigins = SplitCSV(v)
	}
	return cfg
}

// Save writes cfg to config.json.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Store is a concurrency-safe holder for the live configuration.
type Store struct {
	mu  sync.Mutex
	cfg Config
}

// NewStore returns a Store holding cfg.
func NewStore(cfg Config) *Store { return &Store{cfg: cfg} }

// Get returns a copy of the current configuration.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Update applies fn to the configuration and persists the result.
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	fn(&s.cfg)
	cfg := s.cfg
	s.mu.Unlock()
	return Save(cfg)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// IsLoopbackHost reports whether host refers only to the local machine.
func IsLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// SplitCSV splits a comma-separated list, trimming blanks.
func SplitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// IsAllowedOrigin reports whether a browser origin may open a WebSocket
// connection. An empty Origin header (non-browser clients such as curl or
// native apps) is always allowed. Otherwise the origin's scheme and host must
// match an allowlist entry; an entry with no port matches any port.
func IsAllowedOrigin(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	for _, a := range allowed {
		a = strings.TrimSuffix(strings.TrimSpace(a), "/")
		if a == origin {
			return true
		}
		au, err := url.Parse(a)
		if err != nil || au.Scheme == "" || au.Hostname() == "" {
			continue
		}
		if au.Scheme == u.Scheme && au.Hostname() == u.Hostname() &&
			(au.Port() == "" || au.Port() == u.Port()) {
			return true
		}
	}
	return false
}
