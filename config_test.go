package main

import "testing"

func TestIsAllowedOrigin(t *testing.T) {
	allowed := []string{"http://localhost", "https://app.example.com"}
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},                          // native/non-browser clients
		{"http://localhost:3000", true},     // no port on allowlist entry -> any port matches
		{"http://localhost", true},
		{"https://app.example.com", true},
		{"https://evil.com", false},
		{"http://app.example.com", false},   // scheme mismatch
		{"https://app.example.com.evil.com", false}, // must not substring-match host
		{"not-a-url", false},
	}
	for _, c := range cases {
		if got := isAllowedOrigin(c.origin, allowed); got != c.want {
			t.Errorf("isAllowedOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, b ,,c")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"", true},
		{"127.0.0.1", true},
		{"localhost", true},
		{"::1", true},
		{"[::1]", true},
		{"127.0.0.5", true},
		{"0.0.0.0", false},
		{"192.168.1.10", false},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestDefaultConfigBindsLoopback(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", cfg.Host)
	}
	if !isLoopbackHost(cfg.Host) {
		t.Error("default host must be loopback")
	}
}

func TestDefaultConfigAllowsLocalhostOnly(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Port != "8765" {
		t.Errorf("default port = %q, want 8765", cfg.Port)
	}
	if isAllowedOrigin("https://random-website.com", cfg.AllowedOrigins) {
		t.Error("default config must not allow arbitrary origins")
	}
	if !isAllowedOrigin("http://localhost:5173", cfg.AllowedOrigins) {
		t.Error("default config should allow localhost dev servers on any port")
	}
}
