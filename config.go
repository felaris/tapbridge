package main

import (
	"encoding/json"
	"flag"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config holds runtime settings, resolved in increasing priority from:
// built-in defaults -> on-disk config file -> environment variables -> CLI flags.
type Config struct {
	Host           string   `json:"host,omitempty"`
	Port           string   `json:"port,omitempty"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	SelectedReader string   `json:"selectedReader,omitempty"`
	AutoStart      bool     `json:"autoStart"`
}

var (
	flagHost    = flag.String("host", "", "Address to bind the WebSocket server to (default 127.0.0.1, loopback only). Use 0.0.0.0 to expose on the network — only do this if you understand the risk.")
	flagPort    = flag.String("port", "", "WebSocket port to listen on (default 8765)")
	flagOrigins = flag.String("allow-origin", "", "Comma-separated list of allowed WebSocket origins (default: localhost/127.0.0.1 only)")
)

var (
	appConfig   Config
	appConfigMu sync.Mutex
)

func defaultConfig() Config {
	return Config{
		// Bind to loopback only by default: the WebSocket exposes card IDs and
		// accepts no-Origin (non-browser) clients, so it must not be reachable
		// from the LAN unless the operator opts in explicitly.
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

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tapbridge", "config.json"), nil
}

// loadConfig reads the on-disk config (if any) and layers environment
// variable and CLI flag overrides on top. flag.Parse must already have run.
func loadConfig() Config {
	cfg := defaultConfig()

	if path, err := configPath(); err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var fileCfg Config
			if json.Unmarshal(data, &fileCfg) == nil {
				if fileCfg.Host != "" {
					cfg.Host = fileCfg.Host
				}
				if fileCfg.Port != "" {
					cfg.Port = fileCfg.Port
				}
				if len(fileCfg.AllowedOrigins) > 0 {
					cfg.AllowedOrigins = fileCfg.AllowedOrigins
				}
				cfg.SelectedReader = fileCfg.SelectedReader
				cfg.AutoStart = fileCfg.AutoStart
			}
		}
	}

	if *flagHost != "" {
		cfg.Host = *flagHost
	} else if v := os.Getenv("TAPBRIDGE_HOST"); v != "" {
		cfg.Host = v
	}

	if *flagPort != "" {
		cfg.Port = *flagPort
	} else if v := os.Getenv("TAPBRIDGE_PORT"); v != "" {
		cfg.Port = v
	}

	if *flagOrigins != "" {
		cfg.AllowedOrigins = splitCSV(*flagOrigins)
	} else if v := os.Getenv("TAPBRIDGE_ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = splitCSV(v)
	}

	return cfg
}

func saveConfig(cfg Config) error {
	path, err := configPath()
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

func getConfig() Config {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()
	return appConfig
}

func setSelectedReader(name string) {
	appConfigMu.Lock()
	appConfig.SelectedReader = name
	cfg := appConfig
	appConfigMu.Unlock()
	_ = saveConfig(cfg)
}

func setAutoStart(enabled bool) {
	appConfigMu.Lock()
	appConfig.AutoStart = enabled
	cfg := appConfig
	appConfigMu.Unlock()
	_ = saveConfig(cfg)
}

// isLoopbackHost reports whether host refers only to the local machine, so the
// caller can warn when the WebSocket is being exposed to the network.
func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isAllowedOrigin reports whether origin may open a WebSocket connection. An
// empty Origin header (non-browser clients such as curl or native apps) is
// always allowed. Otherwise origin's scheme+host must match an allowlist
// entry; an allowlist entry with no port matches any port on that host.
func isAllowedOrigin(origin string, allowed []string) bool {
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
		if au.Scheme == u.Scheme && au.Hostname() == u.Hostname() {
			if au.Port() == "" || au.Port() == u.Port() {
				return true
			}
		}
	}
	return false
}
