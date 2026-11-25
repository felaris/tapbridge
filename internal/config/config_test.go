package config

import "testing"

func TestIsAllowedOrigin(t *testing.T) {
	allowed := []string{"http://localhost", "https://app.example.com"}
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},                      // native/non-browser clients
		{"http://localhost:3000", true}, // no port on allowlist entry -> any port matches
		{"http://localhost", true},
		{"https://app.example.com", true},
		{"https://evil.com", false},
		{"http://app.example.com", false},           // scheme mismatch
		{"https://app.example.com.evil.com", false}, // must not substring-match host
		{"not-a-url", false},
	}
	for _, c := range cases {
		if got := IsAllowedOrigin(c.origin, allowed); got != c.want {
			t.Errorf("IsAllowedOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	got := SplitCSV(" a, b ,,c")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("SplitCSV length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SplitCSV[%d] = %q, want %q", i, got[i], want[i])
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
		if got := IsLoopbackHost(c.host); got != c.want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestDefaultConfigBindsLoopback(t *testing.T) {
	cfg := Default()
	if cfg.Host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", cfg.Host)
	}
	if !IsLoopbackHost(cfg.Host) {
		t.Error("default host must be loopback")
	}
}

func TestDefaultConfigAllowsLocalhostOnly(t *testing.T) {
	cfg := Default()
	if cfg.Port != "8765" {
		t.Errorf("default port = %q, want 8765", cfg.Port)
	}
	if IsAllowedOrigin("https://random-website.com", cfg.AllowedOrigins) {
		t.Error("default config must not allow arbitrary origins")
	}
	if !IsAllowedOrigin("http://localhost:5173", cfg.AllowedOrigins) {
		t.Error("default config should allow localhost dev servers on any port")
	}
}

func TestApplyOverridesPrecedence(t *testing.T) {
	env := map[string]string{EnvPort: "9001", EnvOrigins: "https://env.example"}
	getenv := func(k string) string { return env[k] }

	cfg := applyOverrides(Default(), Overrides{}, getenv)
	if cfg.Port != "9001" {
		t.Errorf("env port not applied: %q", cfg.Port)
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "https://env.example" {
		t.Errorf("env origins not applied: %v", cfg.AllowedOrigins)
	}

	cfg = applyOverrides(Default(), Overrides{Port: "9002"}, getenv)
	if cfg.Port != "9002" {
		t.Errorf("flag must beat env: got %q", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("unset host must keep default, got %q", cfg.Host)
	}
}

func TestMergeConfigFile(t *testing.T) {
	cfg := merge(Default(), []byte(`{"port":"7000","selectedReader":"R1","autoStart":true}`))
	if cfg.Port != "7000" || cfg.SelectedReader != "R1" || !cfg.AutoStart {
		t.Errorf("file values not merged: %+v", cfg)
	}
	if cfg.Host != "127.0.0.1" || len(cfg.AllowedOrigins) != 4 {
		t.Errorf("missing file values must keep defaults: %+v", cfg)
	}
	if got := merge(Default(), []byte("{not json")); got.Port != "8765" {
		t.Errorf("corrupt file must be ignored, got %+v", got)
	}
}
