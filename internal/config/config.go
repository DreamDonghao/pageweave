package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config contains validated process-wide settings.
type Config struct {
	DataDir                                                       string
	AdminCookieSecure                                             bool
	Host                                                          string
	Port                                                          int
	BrowserPath                                                   string
	MaxConcurrency                                                int
	RequestTimeout, NavigationTimeout, RenderWait                 time.Duration
	MaxHTMLBytes, MaxRequestBytes, MaxOutputChars, MaxScrollSteps int
	LogLevel                                                      slog.Level
}

// Load reads PAGEWEAVE_ variables; invalid or explicitly empty values fail startup.
func Load() (Config, error) {
	c := Config{Host: "0.0.0.0", Port: 7779, BrowserPath: "/usr/bin/chromium", MaxConcurrency: 2, RequestTimeout: 25 * time.Second, NavigationTimeout: 10 * time.Second, RenderWait: 5 * time.Second, MaxHTMLBytes: 2097152, MaxRequestBytes: 16384, MaxOutputChars: 50000, MaxScrollSteps: 3, LogLevel: slog.LevelInfo}
	if v, ok := os.LookupEnv("PAGEWEAVE_DATA_DIR"); ok {
		if v == "" {
			return c, fmt.Errorf("empty PAGEWEAVE_DATA_DIR")
		}
		c.DataDir = v
	} else {
		dir, e := os.UserConfigDir()
		if e != nil {
			return c, e
		}
		c.DataDir = filepath.Join(dir, "pageweave")
	}
	if v, ok := os.LookupEnv("PAGEWEAVE_ADMIN_COOKIE_SECURE"); ok {
		secure, e := strconv.ParseBool(v)
		if e != nil {
			return c, fmt.Errorf("invalid PAGEWEAVE_ADMIN_COOKIE_SECURE")
		}
		c.AdminCookieSecure = secure
	}
	if v, ok := os.LookupEnv("PAGEWEAVE_HOST"); ok {
		c.Host = v
	}
	if v, ok := os.LookupEnv("PAGEWEAVE_BROWSER_PATH"); ok {
		c.BrowserPath = v
	}
	ints := []struct {
		name     string
		dst      *int
		min, max int
	}{
		{"PORT", &c.Port, 1, 65535}, {"MAX_CONCURRENCY", &c.MaxConcurrency, 1, 128},
		{"MAX_HTML_BYTES", &c.MaxHTMLBytes, 1, 64 << 20}, {"MAX_REQUEST_BYTES", &c.MaxRequestBytes, 1, 1 << 20},
		{"MAX_OUTPUT_CHARS", &c.MaxOutputChars, 1, 50000}, {"MAX_SCROLL_STEPS", &c.MaxScrollSteps, 0, 100}}
	for _, f := range ints {
		if v, ok := os.LookupEnv("PAGEWEAVE_" + f.name); ok {
			n, e := strconv.Atoi(v)
			if e != nil || n < f.min || n > f.max {
				return c, fmt.Errorf("invalid PAGEWEAVE_%s", f.name)
			}
			*f.dst = n
		}
	}
	for _, f := range []struct {
		name string
		dst  *time.Duration
	}{{"REQUEST_TIMEOUT_SECONDS", &c.RequestTimeout}, {"NAVIGATION_TIMEOUT_SECONDS", &c.NavigationTimeout}, {"RENDER_WAIT_SECONDS", &c.RenderWait}} {
		if v, ok := os.LookupEnv("PAGEWEAVE_" + f.name); ok {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 300 {
				return c, fmt.Errorf("invalid PAGEWEAVE_%s", f.name)
			}
			*f.dst = time.Duration(n) * time.Second
		}
	}
	if v, ok := os.LookupEnv("PAGEWEAVE_LOG_LEVEL"); ok {
		if e := c.LogLevel.UnmarshalText([]byte(strings.ToUpper(v))); e != nil {
			return c, e
		}
	}
	if net.ParseIP(c.Host) == nil && c.Host != "localhost" {
		return c, fmt.Errorf("invalid PAGEWEAVE_HOST")
	}
	if c.BrowserPath == "" {
		return c, fmt.Errorf("empty PAGEWEAVE_BROWSER_PATH")
	}
	return c, nil
}

// Address returns the HTTP listening address.
func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
