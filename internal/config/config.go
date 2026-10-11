// Package config loads runtime configuration from the environment.
//
// Every knob is optional so that the binary runs with zero configuration in a
// development sandbox while remaining fully tunable for an intranet
// deployment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lyy1119/MyCalendar/internal/calendar"
)

// Environment variable names.
const (
	EnvAddr       = "MYCAL_ADDR"
	EnvDB         = "MYCAL_DB"
	EnvTZ         = "MYCAL_TZ"
	EnvWeekStart  = "MYCAL_WEEK_START"
	EnvCORS       = "MYCAL_CORS_ORIGINS"
	EnvLogLevel   = "MYCAL_LOG_LEVEL"
	EnvTrustProxy = "MYCAL_TRUST_PROXY"
)

// Defaults applied when an environment variable is absent.
const (
	DefaultAddr     = ":8080"
	DefaultDB       = "mycalendar.db"
	DefaultTZ       = "Local"
	DefaultWeekStar = "monday"
	DefaultCORS     = "*"
	DefaultLogLevel = "info"
)

// Config is the fully validated runtime configuration.
type Config struct {
	Addr            string
	DBPath          string
	Timezone        string
	Location        *time.Location
	WeekStart       calendar.WeekStart
	CORSOrigins     []string
	LogLevel        slog.Level
	TrustProxy      bool
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
}

// Load reads the environment and validates it, returning a ready Config.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:            envOr(EnvAddr, DefaultAddr),
		DBPath:          envOr(EnvDB, DefaultDB),
		Timezone:        envOr(EnvTZ, DefaultTZ),
		CORSOrigins:     splitList(envOr(EnvCORS, DefaultCORS)),
		TrustProxy:      envBool(EnvTrustProxy, false),
		ShutdownTimeout: 15 * time.Second,
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     60 * time.Second,
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid %s=%q: %w", EnvTZ, cfg.Timezone, err)
	}
	cfg.Location = loc

	ws, err := calendar.ParseWeekStart(envOr(EnvWeekStart, DefaultWeekStar))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", EnvWeekStart, err)
	}
	cfg.WeekStart = ws

	lvl, err := parseLevel(envOr(EnvLogLevel, DefaultLogLevel))
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = lvl

	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("%s must not be empty", EnvAddr)
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return nil, fmt.Errorf("%s must not be empty", EnvDB)
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid %s=%q: want debug, info, warn or error", EnvLogLevel, s)
	}
}

// AllowAllOrigins reports whether CORS is configured to accept any origin.
func (c *Config) AllowAllOrigins() bool {
	for _, o := range c.CORSOrigins {
		if o == "*" {
			return true
		}
	}
	return false
}
