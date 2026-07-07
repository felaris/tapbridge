package main

import (
	"encoding/json"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config holds runtime settings, resolved in increasing priority from:
// built-in defaults -> on-disk config file -> environment variables -> CLI flags.
type Config struct {
	Port           string   `json:"port,omitempty"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
	SelectedReader string   `json:"selectedReader,omitempty"`
	AutoStart      bool     `json:"autoStart"`
}

var (
	flagPort    = flag.String("port", "", "WebSocket port to listen on (default 8765)")
	flagOrigins = flag.String("allow-origin", "", "Comma-separated list of allowed WebSocket origins (default: localhost/127.0.0.1 only)")
)

var (
	appConfig   Config
	appConfigMu sync.Mutex
)

func defaultConfig() Config {
	return Config{
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
