package extraction

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DreamDonghao/pageweave/internal/network"
)

func TestInvalidBrowserStartup(t *testing.T) {
	c := testConfig(t)
	c.BrowserPath = filepath.Join(t.TempDir(), "missing")
	if _, e := NewBrowser(context.Background(), c, network.Policy{}, quietLog()); e == nil {
		t.Fatal("missing browser accepted")
	}
	c.BrowserPath = filepath.Join(t.TempDir(), "not-executable")
	if e := os.WriteFile(c.BrowserPath, []byte("invalid"), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := NewBrowser(context.Background(), c, network.Policy{}, quietLog()); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("non-executable accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failed startup cleanup hangs")
	}
}
