package config

import (
	"log/slog"
	"testing"

	"github.com/lyy1119/MyCalendar/internal/calendar"
)

// clearEnv unsets every variable Load reads so tests do not leak into each
// other or inherit the developer's shell.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{EnvAddr, EnvDB, EnvTZ, EnvWeekStart, EnvCORS, EnvLogLevel, EnvTrustProxy} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != DefaultAddr {
		t.Errorf("Addr = %q, want %q", cfg.Addr, DefaultAddr)
	}
	if cfg.DBPath != DefaultDB {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, DefaultDB)
	}
	if cfg.Timezone != DefaultTZ {
		t.Errorf("Timezone = %q, want %q", cfg.Timezone, DefaultTZ)
	}
	if cfg.WeekStart != calendar.Monday {
		t.Errorf("WeekStart = %q, want monday", cfg.WeekStart)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if !cfg.AllowAllOrigins() {
		t.Error("AllowAllOrigins = false, want the default to be permissive")
	}
	if cfg.Location == nil {
		t.Error("Location was not resolved")
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvAddr, "127.0.0.1:9090")
	t.Setenv(EnvDB, "/tmp/custom.db")
	t.Setenv(EnvTZ, "Asia/Shanghai")
	t.Setenv(EnvWeekStart, "sunday")
	t.Setenv(EnvLogLevel, "debug")
	t.Setenv(EnvCORS, "http://a.test, http://b.test ,")
	t.Setenv(EnvTrustProxy, "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.DBPath != "/tmp/custom.db" {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if got := cfg.Location.String(); got != "Asia/Shanghai" {
		t.Errorf("Location = %q", got)
	}
	if cfg.WeekStart != calendar.Sunday {
		t.Errorf("WeekStart = %q, want sunday", cfg.WeekStart)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
	if cfg.AllowAllOrigins() {
		t.Error("AllowAllOrigins = true for an explicit origin list")
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Errorf("CORSOrigins = %v, want two entries with blanks dropped", cfg.CORSOrigins)
	}
	if !cfg.TrustProxy {
		t.Error("TrustProxy = false, want true")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"unknown timezone", EnvTZ, "Mars/Olympus"},
		{"malformed timezone", EnvTZ, "not a zone"},
		{"bad week start", EnvWeekStart, "tuesday"},
		{"bad log level", EnvLogLevel, "chatty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load succeeded with %s=%q, want an error", tc.key, tc.value)
			}
		})
	}
}

func TestAllowAllOriginsWildcard(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvCORS, "*")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AllowAllOrigins() {
		t.Error("AllowAllOrigins = false for a wildcard origin list")
	}
}
