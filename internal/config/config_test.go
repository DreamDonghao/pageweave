package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.Port != 7779 || c.MaxConcurrency != 2 || c.RequestTimeout != 25*time.Second || c.MaxHTMLBytes != 2097152 {
		t.Fatal(c)
	}
}
func TestInvalid(t *testing.T) {
	for _, name := range []string{"PORT", "MAX_CONCURRENCY", "REQUEST_TIMEOUT_SECONDS", "MAX_HTML_BYTES", "MAX_OUTPUT_CHARS", "MAX_SCROLL_STEPS", "LOG_LEVEL", "HOST", "BROWSER_PATH", "DATA_DIR", "ADMIN_COOKIE_SECURE"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PAGEWEAVE_"+name, "")
			if _, e := Load(); e == nil {
				t.Fatal("accepted empty config")
			}
		})
	}
}
func TestOverrides(t *testing.T) {
	t.Setenv("PAGEWEAVE_MAX_SCROLL_STEPS", "0")
	t.Setenv("PAGEWEAVE_LOG_LEVEL", "debug")
	t.Setenv("PAGEWEAVE_PORT", "9090")
	c, e := Load()
	if e != nil || c.MaxScrollSteps != 0 || c.Port != 9090 {
		t.Fatal(c, e)
	}
}
