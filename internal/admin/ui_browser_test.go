//go:build browser

package admin

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func TestAdminBrowserLoginAndSave(t *testing.T) {
	manager, token, _ := testManager(t)
	api := httptest.NewServer(manager)
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := os.Getenv("PAGEWEAVE_BROWSER_PATH")
	if path == "" {
		path = "/usr/bin/chromium"
	}
	l := launcher.New().Bin(path).UserDataDir(t.TempDir()).NoSandbox(false).Leakless(false).Context(ctx)
	url, e := l.Launch()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { l.Kill(); l.Cleanup() }()
	browser := rod.New().Context(ctx).ControlURL(url)
	if e = browser.Connect(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		clean, cc := context.WithTimeout(context.Background(), time.Second)
		defer cc()
		if e := browser.Context(clean).Close(); e != nil {
			t.Log("browser close:", e)
		}
	}()
	page, e := browser.Page(proto.TargetCreateTarget{URL: api.URL + "/admin/"})
	if e != nil {
		t.Fatal(e)
	}
	if e = page.WaitLoad(); e != nil {
		t.Fatal(e)
	}

	// Pass the token as a bound argument, never embed it in executable JavaScript or a URL.
	if _, e = page.Eval(`(token) => {document.querySelector('#token').value=token;document.querySelector('#login-form').requestSubmit()}`, token); e != nil {
		t.Fatal(e)
	}
	if e = page.Wait(rod.Eval(`() => !document.querySelector('#dashboard').classList.contains('hidden') && document.querySelector('#max_concurrency').value!==''`)); e != nil {
		t.Fatal(e)
	}
	if _, e = page.Eval(`() => {document.querySelector('#max_concurrency').value='4';document.querySelector('#settings-form').requestSubmit()}`); e != nil {
		t.Fatal(e)
	}
	if e = page.Wait(rod.Eval(`() => document.querySelector('#message').textContent.includes('配置已保存')`)); e != nil {
		t.Fatal(e)
	}
	manager.mu.Lock()
	saved := manager.saved.MaxConcurrency
	manager.mu.Unlock()
	if saved != 4 {
		t.Fatal("UI did not save settings:", saved)
	}
	if dir := os.Getenv("PAGEWEAVE_TEST_ARTIFACT_DIR"); dir != "" {
		data, e := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, "admin-dashboard.png"), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	obj, e := page.Eval(`() => document.querySelector('#token').value`)
	if e != nil || strings.TrimSpace(obj.Value.Str()) != "" {
		t.Fatal("token remained in login input")
	}
}
