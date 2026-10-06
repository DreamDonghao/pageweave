package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/DreamDonghao/pageweave/internal/config"
)

// Settings contains operational values editable through the administration UI.
// Listener, browser path and network policy remain deployment settings.
type Settings struct {
	MaxConcurrency           int    `json:"max_concurrency"`
	RequestTimeoutSeconds    int    `json:"request_timeout_seconds"`
	NavigationTimeoutSeconds int    `json:"navigation_timeout_seconds"`
	RenderWaitSeconds        int    `json:"render_wait_seconds"`
	MaxHTMLBytes             int    `json:"max_html_bytes"`
	MaxRequestBytes          int    `json:"max_request_bytes"`
	MaxOutputChars           int    `json:"max_output_chars"`
	MaxScrollSteps           int    `json:"max_scroll_steps"`
	LogLevel                 string `json:"log_level"`
}

func FromConfig(c config.Config) Settings {
	return Settings{c.MaxConcurrency, int(c.RequestTimeout / time.Second), int(c.NavigationTimeout / time.Second), int(c.RenderWait / time.Second), c.MaxHTMLBytes, c.MaxRequestBytes, c.MaxOutputChars, c.MaxScrollSteps, c.LogLevel.String()}
}
func (s Settings) Apply(c config.Config) (config.Config, error) {
	for _, v := range []struct {
		name            string
		value, min, max int
	}{
		{"max_concurrency", s.MaxConcurrency, 1, 128}, {"request_timeout_seconds", s.RequestTimeoutSeconds, 1, 300}, {"navigation_timeout_seconds", s.NavigationTimeoutSeconds, 1, 300}, {"render_wait_seconds", s.RenderWaitSeconds, 1, 300},
		{"max_html_bytes", s.MaxHTMLBytes, 1, 64 << 20}, {"max_request_bytes", s.MaxRequestBytes, 1, 1 << 20}, {"max_output_chars", s.MaxOutputChars, 1, 50000}, {"max_scroll_steps", s.MaxScrollSteps, 0, 100},
	} {
		if v.value < v.min || v.value > v.max {
			return c, fmt.Errorf("%s must be %d..%d", v.name, v.min, v.max)
		}
	}
	var level slog.Level
	if e := level.UnmarshalText([]byte(s.LogLevel)); e != nil {
		return c, fmt.Errorf("invalid log_level")
	}
	c.MaxConcurrency = s.MaxConcurrency
	c.RequestTimeout = time.Duration(s.RequestTimeoutSeconds) * time.Second
	c.NavigationTimeout = time.Duration(s.NavigationTimeoutSeconds) * time.Second
	c.RenderWait = time.Duration(s.RenderWaitSeconds) * time.Second
	c.MaxHTMLBytes = s.MaxHTMLBytes
	c.MaxRequestBytes = s.MaxRequestBytes
	c.MaxOutputChars = s.MaxOutputChars
	c.MaxScrollSteps = s.MaxScrollSteps
	c.LogLevel = level
	return c, nil
}
func DecodeSettings(data []byte) (Settings, error) {
	var s Settings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&s); e != nil {
		return s, e
	}
	if e := decoder.Decode(new(any)); e != io.EOF {
		return s, fmt.Errorf("one JSON object required")
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(data, &fields); e != nil || fields == nil {
		return s, fmt.Errorf("settings must be an object")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return s, fmt.Errorf("null settings are not supported")
		}
	}
	return s, nil
}

// LoadSettings overlays saved operational settings on deployment defaults.
func LoadSettings(c config.Config) (config.Config, error) {
	path := filepath.Join(c.DataDir, "settings.json")
	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return c, nil
	}
	if statErr != nil {
		return c, statErr
	}
	if info.Size() > 16384 {
		return c, fmt.Errorf("saved settings too large")
	}
	data, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	s, e := DecodeSettings(data)
	if e != nil {
		return c, fmt.Errorf("saved settings: %w", e)
	}
	return s.Apply(c)
}
func saveSettings(dir string, s Settings) error {
	data, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	file, e := os.CreateTemp(dir, "settings-*.tmp")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	if e = file.Chmod(0600); e == nil {
		_, e = file.Write(append(data, '\n'))
	}
	if e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, filepath.Join(dir, "settings.json"))
}
