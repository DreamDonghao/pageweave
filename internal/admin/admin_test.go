package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DreamDonghao/pageweave/internal/config"
)

func testManager(t *testing.T) (*Manager, string, config.Config) {
	t.Helper()
	c, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	c.DataDir = t.TempDir()
	m, token, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	m.SetRuntime(c, "running", func() bool { return true })
	return m, token, c
}
func call(m *Manager, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.test")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, m *Manager, token string) (*http.Cookie, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": token})
	w := call(m, "POST", "/admin/api/login", string(body), nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal(cookies)
	}
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	return cookies[0], result.CSRF
}
func TestTokenLoginAndSessions(t *testing.T) {
	m, token, c := testManager(t)
	if len(token) < 40 {
		t.Fatal("short token")
	}
	if w := call(m, "GET", "/admin/api/config", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call(m, "POST", "/admin/api/login", `{"token":"wrong"}`, nil, ""); w.Code != 401 || strings.Contains(w.Body.String(), token) {
		t.Fatal(w.Code, w.Body)
	}
	cookie, csrf := login(t, m, token)
	if w := call(m, "GET", "/admin/api/config", "", cookie, ""); w.Code != 200 || strings.Contains(w.Body.String(), token) {
		t.Fatal(w.Code, w.Body)
	}
	tampered := *cookie
	tampered.Value += "x"
	if w := call(m, "GET", "/admin/api/config", "", &tampered, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	other, otherToken, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	if otherToken == token {
		t.Fatal("token reused after startup")
	}
	if w := call(other, "GET", "/admin/api/config", "", cookie, ""); w.Code != 401 {
		t.Fatal("old startup session accepted")
	}
	if w := call(m, "POST", "/admin/api/logout", `{}`, cookie, csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(m, "GET", "/admin/api/config", "", cookie, ""); w.Code != 401 {
		t.Fatal("logged out session accepted")
	}
}
func TestSettingsSaveApplyAndPersistence(t *testing.T) {
	m, token, c := testManager(t)
	cookie, csrf := login(t, m, token)
	settings := FromConfig(c)
	settings.MaxConcurrency = 4
	settings.RequestTimeoutSeconds = 40
	body, _ := json.Marshal(settings)
	if w := call(m, "PUT", "/admin/api/config", string(body), cookie, ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	w := call(m, "PUT", "/admin/api/config", string(body), cookie, csrf)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	loaded, e := LoadSettings(c)
	if e != nil || loaded.MaxConcurrency != 4 || loaded.RequestTimeout != 40*time.Second {
		t.Fatal(loaded, e)
	}
	if m.active.MaxConcurrency != 2 {
		t.Fatal("active settings mutated before reload")
	}
	w = call(m, "POST", "/admin/api/apply", `{}`, cookie, csrf)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	select {
	case <-m.ApplyRequested():
	case <-time.After(time.Second):
		t.Fatal("missing reload signal")
	}
	if w = call(m, "POST", "/admin/api/apply", `{}`, cookie, csrf); w.Code != 409 {
		t.Fatal("duplicate apply accepted", w.Code)
	}
	if w = call(m, "PUT", "/admin/api/config", string(body), cookie, csrf); w.Code != 409 {
		t.Fatal("save during apply accepted", w.Code)
	}
	m.SetRuntime(loaded, "running", func() bool { return true })
	if w = call(m, "GET", "/admin/api/config", "", cookie, ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"pending":false`)) {
		t.Fatal(w.Code, w.Body)
	}
	settings.MaxConcurrency = 0
	body, _ = json.Marshal(settings)
	if w = call(m, "PUT", "/admin/api/config", string(body), cookie, csrf); w.Code != 422 {
		t.Fatal(w.Code)
	}
	if w = call(m, "PUT", "/admin/api/config", `{"browser_path":"/tmp/x"}`, cookie, csrf); w.Code != 422 {
		t.Fatal("deployment override accepted")
	}
}
func TestCrossOriginAndLoginLimit(t *testing.T) {
	m, token, _ := testManager(t)
	cookie, csrf := login(t, m, token)
	r := httptest.NewRequest("POST", "http://example.test/admin/api/apply", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Origin", "http://evil.test")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin apply allowed")
	}
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "http://example.test/admin/api/login", strings.NewReader(`{"token":"wrong"}`))
		r.RemoteAddr = "192.0.2.1:" + strings.Repeat("1", i+1)
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		m.ServeHTTP(w, r)
		if i < 5 && w.Code != 401 {
			t.Fatal(i, w.Code)
		}
		if i == 5 && w.Code != 429 {
			t.Fatal("source-port rate-limit bypass", w.Code)
		}
	}
}
func TestUIAndInvalidSettings(t *testing.T) {
	m, _, _ := testManager(t)
	w := call(m, "GET", "/admin/", "", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Token") || w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal(w.Code)
	}
	for _, data := range []string{`null`, `{"max_concurrency":null}`, `{} {}`, `{"extra":1}`} {
		if _, e := DecodeSettings([]byte(data)); e == nil {
			t.Fatal(data)
		}
	}
}

func TestExpiredAndSecureSession(t *testing.T) {
	m, token, c := testManager(t)
	c.AdminCookieSecure = true
	m.SetRuntime(c, "running", func() bool { return true })
	cookie, _ := login(t, m, token)
	if !cookie.Secure {
		t.Fatal("secure cookie not enabled")
	}
	expired := session{Expires: time.Now().Add(-time.Minute).Unix(), CSRF: "expired", ID: "old"}
	data, _ := json.Marshal(expired)
	body := base64.RawURLEncoding.EncodeToString(data)
	cookie.Value = body + "." + m.mac(body)
	if w := call(m, "GET", "/admin/api/config", "", cookie, ""); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}
