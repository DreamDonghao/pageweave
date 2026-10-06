// Package admin serves token-authenticated operational configuration.
package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DreamDonghao/pageweave/internal/buildinfo"
	"github.com/DreamDonghao/pageweave/internal/config"
)

//go:embed ui.html
var ui []byte

const cookieName = "pageweave_admin"
const sessionTTL = 12 * time.Hour

type session struct {
	Expires int64  `json:"expires"`
	CSRF    string `json:"csrf"`
	ID      string `json:"id"`
}
type attempt struct {
	Count int
	Reset time.Time
}

// Manager owns startup authentication, saved settings and the reload signal.
type Manager struct {
	mu         sync.Mutex
	tokenHash  [32]byte
	sessionKey []byte
	active     config.Config
	saved      Settings
	state      string
	apply      chan struct{}
	ready      func() bool
	failures   map[string]attempt
	revoked    map[string]int64
	updating   bool
}

// New creates a fresh startup token. The caller delivers it once to the operator.
func New(c config.Config) (*Manager, string, error) {
	if e := os.MkdirAll(c.DataDir, 0700); e != nil {
		return nil, "", fmt.Errorf("admin data directory: %w", e)
	}
	token, e := randomString(32)
	if e != nil {
		return nil, "", e
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		return nil, "", e
	}
	return &Manager{tokenHash: sha256.Sum256([]byte(token)), sessionKey: key, active: c, saved: FromConfig(c), state: "starting", apply: make(chan struct{}, 1), failures: make(map[string]attempt), revoked: make(map[string]int64)}, token, nil
}
func randomString(size int) (string, error) {
	b := make([]byte, size)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ApplyRequested signals an explicit operator-requested configuration reload.
func (m *Manager) ApplyRequested() <-chan struct{} { return m.apply }

// SetRuntime replaces only operational status; credentials survive in-process reloads.
func (m *Manager) SetRuntime(c config.Config, state string, ready func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = c
	m.state = state
	m.ready = ready
	if state == "running" {
		m.updating = false
	}
}
func (m *Manager) mac(data string) string {
	h := hmac.New(sha256.New, m.sessionKey)
	h.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (m *Manager) readSession(r *http.Request) (session, bool) {
	var result session
	c, e := r.Cookie(cookieName)
	if e != nil || len(c.Value) > 1024 {
		return result, false
	}
	body, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(m.mac(body))) {
		return result, false
	}
	data, e := base64.RawURLEncoding.DecodeString(body)
	if e != nil || json.Unmarshal(data, &result) != nil || result.Expires <= time.Now().Unix() {
		return result, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, revoked := m.revoked[result.ID]
	return result, !revoked
}
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func reject(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]any{"error": map[string]string{"message": message}})
}
func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return fmt.Errorf("application/json required")
	}
	body := http.MaxBytesReader(w, r.Body, 16384)
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(target); e != nil {
		return e
	}
	if e := decoder.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("one JSON object required")
	}
	return nil
}
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.URL.Path == "/admin/" {
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			reject(w, 405, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		_, _ = w.Write(ui)
		return
	}
	if r.URL.Path == "/admin/api/login" {
		m.login(w, r)
		return
	}
	allowedMethod := "GET"
	switch r.URL.Path {
	case "/admin/api/session", "/admin/api/config":
	case "/admin/api/logout", "/admin/api/apply":
		allowedMethod = "POST"
	default:
		reject(w, 404, "not found")
		return
	}
	if r.URL.Path == "/admin/api/config" && r.Method == "PUT" {
		allowedMethod = "PUT"
	}
	if r.Method != allowedMethod {
		w.Header().Set("Allow", allowedMethod)
		reject(w, 405, "method not allowed")
		return
	}
	s, ok := m.readSession(r)
	if !ok {
		reject(w, 401, "请先使用启动 Token 登录")
		return
	}
	if r.Method != "GET" && (!sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1) {
		reject(w, 403, "request verification failed")
		return
	}
	switch r.URL.Path {
	case "/admin/api/session":
		reply(w, 200, map[string]any{"csrf_token": s.CSRF, "expires_at": s.Expires})
	case "/admin/api/config":
		if r.Method == "PUT" {
			m.save(w, r)
			return
		}
		m.mu.Lock()
		active, saved, state, ready, updating := m.active, m.saved, m.state, m.ready, m.updating
		m.mu.Unlock()
		isReady := ready != nil && ready()
		reply(w, 200, map[string]any{"version": buildinfo.Version(), "active": FromConfig(active), "saved": saved, "pending": FromConfig(active) != saved, "state": state, "ready": isReady, "applying": updating, "listener": active.Address()})
	case "/admin/api/logout":
		m.mu.Lock()
		now := time.Now().Unix()
		for id, expires := range m.revoked {
			if expires <= now {
				delete(m.revoked, id)
			}
		}
		if len(m.revoked) >= 1024 {
			m.mu.Unlock()
			reject(w, 429, "too many session changes")
			return
		}
		m.revoked[s.ID] = s.Expires
		m.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/admin/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/apply":
		m.mu.Lock()
		if m.updating {
			m.mu.Unlock()
			reject(w, 409, "配置正在应用，请稍后")
			return
		}
		m.updating = true
		m.mu.Unlock()
		reply(w, 202, map[string]bool{"restarting": true})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		m.apply <- struct{}{}
	}
}
func (m *Manager) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		reject(w, 405, "method not allowed")
		return
	}
	if !sameOrigin(r) {
		reject(w, 403, "request origin rejected")
		return
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	m.mu.Lock()
	now := time.Now()
	entry := m.failures[host]
	if !entry.Reset.After(now) {
		entry = attempt{Reset: now.Add(time.Minute)}
	}
	if entry.Count >= 5 {
		m.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		reject(w, 429, "登录尝试过多，请稍后重试")
		return
	}
	if len(m.failures) >= 256 {
		for ip, value := range m.failures {
			if !value.Reset.After(now) {
				delete(m.failures, ip)
			}
		}
		if len(m.failures) >= 256 {
			m.mu.Unlock()
			reject(w, 429, "登录请求过多")
			return
		}
	}
	entry.Count++
	m.failures[host] = entry
	m.mu.Unlock()
	var input struct {
		Token string `json:"token"`
	}
	if e := readJSON(w, r, &input); e != nil || len(input.Token) > 256 {
		reject(w, 400, "invalid login request")
		return
	}
	hash := sha256.Sum256([]byte(input.Token))
	if subtle.ConstantTimeCompare(hash[:], m.tokenHash[:]) != 1 {
		reject(w, 401, "Token 不正确或已失效")
		return
	}
	csrf, e := randomString(32)
	if e != nil {
		reject(w, 500, "session unavailable")
		return
	}
	id, e := randomString(24)
	if e != nil {
		reject(w, 500, "session unavailable")
		return
	}
	s := session{Expires: now.Add(sessionTTL).Unix(), CSRF: csrf, ID: id}
	data, e := json.Marshal(s)
	if e != nil {
		reject(w, 500, "session unavailable")
		return
	}
	body := base64.RawURLEncoding.EncodeToString(data)
	m.mu.Lock()
	delete(m.failures, host)
	secure := m.active.AdminCookieSecure
	m.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: body + "." + m.mac(body), Path: "/admin/", HttpOnly: true, Secure: secure || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL / time.Second)})
	reply(w, 200, map[string]any{"csrf_token": csrf, "expires_at": s.Expires})
}
func (m *Manager) save(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 16384)
	defer body.Close()
	data, e := io.ReadAll(body)
	if e != nil {
		reject(w, 400, "invalid settings request")
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		reject(w, 415, "application/json required")
		return
	}
	settings, e := DecodeSettings(data)
	if e != nil {
		reject(w, 422, "配置格式无效")
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updating {
		reject(w, 409, "配置正在应用，请稍后")
		return
	}
	if _, e = settings.Apply(m.active); e != nil {
		reject(w, 422, e.Error())
		return
	}
	if e = saveSettings(m.active.DataDir, settings); e != nil {
		reject(w, 500, "配置保存失败，请检查数据目录权限")
		return
	}
	m.saved = settings
	reply(w, 200, map[string]any{"saved": settings, "pending": FromConfig(m.active) != settings})
}
