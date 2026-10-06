//go:build browser

package extraction

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/DreamDonghao/pageweave/internal/network"
)

func fixture(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/dynamic", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><title>动态测试</title><body><main></main><script>setTimeout(()=>fetch('/data').then(r=>r.text()).then(s=>document.querySelector('main').innerHTML='<h1>动态标题</h1><p id="loaded">'+s+'</p>'),250);setInterval(()=>fetch('/ping'),100)</script></body></html>`)
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "JSAFTERLOAD唯一内容") })
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "pong") })
	mux.HandleFunc("/shell-delayed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<a href="#results">Skip to content</a><p>Accessibility Feedback</p><div id="results"></div><script>setTimeout(()=>document.querySelector('#results').innerHTML='<h2>DELAYEDSEARCHRESULT</h2>',650)</script>`)
	})
	mux.HandleFunc("/shell-only", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<a>Skip to content</a><p>Accessibility Feedback</p>`)
	})
	mux.HandleFunc("/placeholder", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>Loading...</main>`)
	})
	mux.HandleFunc("/changing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>CHANGINGCONTENT</main><script>let n=0;setInterval(()=>document.querySelector('main').innerText='CHANGINGCONTENT '+(++n),50)</script>`)
	})
	mux.HandleFunc("/selector", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>initial</main><div id="target" style="display:none"></div><script>setTimeout(()=>document.querySelector('#target').style.display='block',250);setTimeout(()=>document.querySelector('#target').innerText='SELECTORVISIBLENONEMPTY',700)</script>`)
	})
	mux.HandleFunc("/websocket", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>WEBSOCKETBLOCKEDCONTENT</main><script>try{new WebSocket(location.origin.replace('http','ws')+'/ws')}catch(e){};try{new Worker('/worker.js')}catch(e){};try{navigator.serviceWorker.register('/worker.js')}catch(e){}</script>`)
	})
	mux.HandleFunc("/worker.js", func(w http.ResponseWriter, r *http.Request) {
		t.Error("blocked worker script reached fixture")
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, `new WebSocket(location.origin.replace('http','ws')+'/ws')`)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		t.Error("WebSocket handshake reached fixture")
		w.WriteHeader(400)
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body><main></main></body></html>")
	})
	mux.HandleFunc("/hidden", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<style>.hide{display:none}</style><main><p>VISIBLEMARKER</p><p class="hide">CSSHIDDENMARKER</p></main>`)
	})
	mux.HandleFunc("/nonhtml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"hello":1}`)
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(503)
		fmt.Fprint(w, "<p>upstream error</p>")
	})
	mux.HandleFunc("/redirect-file", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "file:///etc/passwd", 302) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1:1/", 302) })
	mux.HandleFunc("/subresource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>PUBLICCONTENTMARKER</main><img src="http://127.0.0.1:1/no">`)
	})
	mux.HandleFunc("/storage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main id="out"></main><script>document.querySelector('main').innerText='STORAGE:'+localStorage.getItem('seen')+' COOKIE:'+document.cookie;localStorage.setItem('seen','yes');document.cookie='seen=yes'</script>`)
	})
	mux.HandleFunc("/scroll", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<style>p{height:100vh}</style><main><p>SCROLLINITIAL</p><p>START</p></main><script>let n=0;window.addEventListener('scroll',()=>{document.querySelector('main').insertAdjacentHTML('beforeend','<p>SCROLLITEM'+(++n)+'</p>')})</script>`)
	})
	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<main>"+strings.Repeat("x", 5000)+"</main>")
	})
	mux.HandleFunc("/loading", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<main>LONGRESOURCECONTENT</main><img src="/slow">`)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
func testBrowser(t *testing.T) (*Browser, *Service, *httptest.Server) {
	t.Helper()
	c := testConfig(t)
	if path := os.Getenv("PAGEWEAVE_BROWSER_PATH"); path != "" {
		c.BrowserPath = path
	}
	c.RequestTimeout = 4 * time.Second
	c.RenderWait = time.Second
	c.NavigationTimeout = 2 * time.Second
	srv := fixture(t)
	policy := network.Policy{FixtureURL: srv.URL}
	b, e := NewBrowser(context.Background(), c, policy, quietLog())
	if e != nil {
		t.Fatal(e)
	}
	service := NewService(c, b, policy, quietLog())
	t.Cleanup(func() { service.Shutdown(context.Background()); b.Close() })
	return b, service, srv
}
func expectCode(t *testing.T, e error, code string) {
	t.Helper()
	var api *Error
	if !errors.As(e, &api) || api.Code != code {
		t.Fatalf("expected %s, got %v", code, e)
	}
}
func TestBrowserDynamicAndIsolation(t *testing.T) {
	b, s, srv := testBrowser(t)
	for _, path := range []string{"/dynamic", "/loading", "/hidden", "/subresource", "/websocket"} {
		t.Run(path, func(t *testing.T) {
			r := DefaultRequest()
			r.URL = srv.URL + path
			r.ContentScope = "full"
			out, e := s.Extract(context.Background(), r)
			if e != nil {
				t.Fatal(e)
			}
			want := map[string]string{"/dynamic": "JSAFTERLOAD唯一内容", "/loading": "LONGRESOURCECONTENT", "/hidden": "VISIBLEMARKER", "/subresource": "PUBLICCONTENTMARKER", "/websocket": "WEBSOCKETBLOCKEDCONTENT"}[path]
			if !strings.Contains(out.Content, want) {
				t.Fatal(out.Content)
			}
			if path == "/hidden" && strings.Contains(out.Content, "CSSHIDDENMARKER") {
				t.Fatal(out.Content)
			}
		})
	}
	r := DefaultRequest()
	r.URL = srv.URL + "/dynamic"
	selector := "#loaded"
	r.WaitForSelector = &selector
	if out, e := s.Extract(context.Background(), r); e != nil || !strings.Contains(out.Content, "JSAFTERLOAD") {
		t.Fatal(out, e)
	}
	for i := 0; i < 2; i++ {
		r := DefaultRequest()
		r.URL = srv.URL + "/storage"
		out, e := s.Extract(context.Background(), r)
		if e != nil || !strings.Contains(out.Content, "STORAGE:null COOKIE:") || strings.Contains(out.Content, "seen=yes") {
			t.Fatal(out, e)
		}
	}
	root := browserRoot(b)
	contexts, e := proto.TargetGetBrowserContexts{}.Call(root)
	if e != nil || len(contexts.BrowserContextIDs) != 0 {
		t.Fatal(contexts, e)
	}
}
func browserRoot(b *Browser) *rod.Browser { b.mu.RLock(); defer b.mu.RUnlock(); return b.root }
func TestBrowserFailuresAndCleanup(t *testing.T) {
	b, s, srv := testBrowser(t)
	for _, tt := range []struct{ path, code string }{{"/nonhtml", "upstream_error"}, {"/error", "upstream_error"}, {"/redirect", "blocked_url"}, {"/redirect-file", "blocked_url"}, {"/empty", "no_content"}, {"/placeholder", "no_content"}} {
		t.Run(tt.path, func(t *testing.T) {
			r := DefaultRequest()
			r.URL = srv.URL + tt.path
			_, e := s.Extract(context.Background(), r)
			expectCode(t, e, tt.code)
		})
	}
	r := DefaultRequest()
	r.URL = srv.URL + "/dynamic"
	sel := "#never"
	r.WaitForSelector = &sel
	_, e := s.Extract(context.Background(), r)
	expectCode(t, e, "extraction_timeout")
	sel = "["
	_, e = s.Extract(context.Background(), r)
	expectCode(t, e, "invalid_request")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r.WaitForSelector = nil
	_, e = s.Extract(ctx, r)
	expectCode(t, e, "extraction_timeout")
	r.URL = srv.URL + "/hidden"
	if _, e = s.Extract(context.Background(), r); e != nil {
		t.Fatal("next request:", e)
	}
	if len(s.slots) != 0 {
		t.Fatal("slot leak")
	}
	contexts, e := proto.TargetGetBrowserContexts{}.Call(browserRoot(b))
	if e != nil || len(contexts.BrowserContextIDs) != 0 {
		t.Fatal("context leak", contexts, e)
	}
	r.URL = srv.URL + "/large"
	s.cfg.MaxHTMLBytes = 1000
	b.cfg.MaxHTMLBytes = 1000
	_, e = s.Extract(context.Background(), r)
	expectCode(t, e, "content_too_large")
}
func TestBrowserScrollAndRecovery(t *testing.T) {
	b, s, srv := testBrowser(t)
	r := DefaultRequest()
	r.URL = srv.URL + "/scroll"
	r.Scroll = true
	r.ContentScope = "full"
	out, e := s.Extract(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.Content, "SCROLLITEM") || !strings.Contains(strings.Join(out.Warnings, ","), "scroll_limit_reached") {
		t.Fatal(out)
	}
	b.mu.RLock()
	old := b.root
	l := b.launch
	b.mu.RUnlock()
	l.Kill()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.RLock()
		root, ready := b.root, b.ready
		b.mu.RUnlock()
		if ready && root != old {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !b.Ready() || browserRoot(b) == old {
		t.Fatal("browser not recovered")
	}
	r.URL = srv.URL + "/dynamic"
	r.Scroll = false
	if _, e = s.Extract(context.Background(), r); e != nil {
		t.Fatal(e)
	}
}
func TestBrowserHTTPHandler(t *testing.T) {
	_, s, srv := testBrowser(t)
	h := Handler{Service: s, Config: s.cfg, Log: quietLog()}
	body := strings.NewReader(fmt.Sprintf(`{"url":%q,"wait_for_selector":"#loaded"}`, srv.URL+"/dynamic"))
	req := httptest.NewRequest("POST", "/extract", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "JSAFTERLOAD唯一内容") {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	t.Log("dynamic response:", recorder.Body.String())
}
func TestBrowserClientDisconnect(t *testing.T) {
	b, s, srv := testBrowser(t)
	api := httptest.NewServer(Handler{Service: s, Config: s.cfg, Log: quietLog()})
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", api.URL, strings.NewReader(fmt.Sprintf(`{"url":%q,"wait_for_selector":"#never"}`, srv.URL+"/dynamic")))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	if res, e := http.DefaultClient.Do(req); e == nil {
		res.Body.Close()
		t.Fatal("cancelled client unexpectedly completed")
	}
	deadline := time.Now().Add(3 * time.Second)
	clean := false
	for time.Now().Before(deadline) {
		contexts, e := proto.TargetGetBrowserContexts{}.Call(browserRoot(b))
		if e == nil && len(contexts.BrowserContextIDs) == 0 && len(s.slots) == 0 {
			clean = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !clean {
		t.Fatal("client disconnect leaked browser context or slot")
	}
	r := DefaultRequest()
	r.URL = srv.URL + "/dynamic"
	if _, e = s.Extract(context.Background(), r); e != nil {
		t.Fatal("next request failed:", e)
	}
}
func TestBrowserCrashInFlight(t *testing.T) {
	b, s, srv := testBrowser(t)
	b.cfg.RenderWait = 5 * time.Second
	s.cfg.RequestTimeout = 10 * time.Second
	r := DefaultRequest()
	r.URL = srv.URL + "/empty"
	selector := "#never"
	r.WaitForSelector = &selector
	done := make(chan error, 1)
	go func() { _, e := s.Extract(context.Background(), r); done <- e }()
	deadline := time.Now().Add(time.Second)
	for len(s.slots) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	b.mu.RLock()
	l := b.launch
	old := b.root
	b.mu.RUnlock()
	l.Kill()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("crashed request succeeded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("crashed request did not stop")
	}
	if len(s.slots) != 0 {
		t.Fatal("crash leaked slot")
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if b.Ready() && browserRoot(b) != old {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("recovery not completed")
}

func TestBrowserWaitLimitAndSelector(t *testing.T) {
	_, s, srv := testBrowser(t)
	r := DefaultRequest()
	r.URL = srv.URL + "/changing"
	out, e := s.Extract(context.Background(), r)
	if e != nil || !strings.Contains(out.Content, "CHANGINGCONTENT") || !strings.Contains(strings.Join(out.Warnings, ","), "render_wait_limit_reached") {
		t.Fatal(out, e)
	}
	r.URL = srv.URL + "/selector"
	r.ContentScope = "full"
	selector := "#target"
	r.WaitForSelector = &selector
	out, e = s.Extract(context.Background(), r)
	if e != nil || !strings.Contains(out.Content, "SELECTORVISIBLENONEMPTY") {
		t.Fatal(out, e)
	}
}
func TestBrowserRecoveryExhaustion(t *testing.T) {
	c := testConfig(t)
	if path := os.Getenv("PAGEWEAVE_BROWSER_PATH"); path != "" {
		c.BrowserPath = path
	}
	alias := filepath.Join(t.TempDir(), "chromium")
	if e := os.Symlink(c.BrowserPath, alias); e != nil {
		t.Fatal(e)
	}
	c.BrowserPath = alias
	b, e := NewBrowser(context.Background(), c, network.Policy{}, quietLog())
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if e = os.Remove(alias); e != nil {
		t.Fatal(e)
	}
	b.mu.RLock()
	l := b.launch
	b.mu.RUnlock()
	l.Kill()
	select {
	case e := <-b.Fatal():
		if e == nil || b.Ready() {
			t.Fatal("recovery did not fail safely")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("recovery did not stop after three attempts")
	}
}
func TestBrowserShutdownResources(t *testing.T) {
	b, s, _ := testBrowser(t)
	b.mu.RLock()
	dir := b.dir
	pid := b.launch.PID()
	b.mu.RUnlock()
	s.Shutdown(context.Background())
	b.Close()
	if b.Ready() || s.Ready() {
		t.Fatal("ready after shutdown")
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("temporary directory remains:", e)
	}
	if runtime.GOOS == "linux" {
		p, e := os.FindProcess(pid)
		if e == nil && p.Signal(syscall.Signal(0)) == nil {
			t.Fatal("browser process remains")
		}
	}
}
func TestBrowserSandboxStatus(t *testing.T) {
	b, _, _ := testBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	page, e := browserRoot(b).Context(ctx).Page(proto.TargetCreateTarget{URL: "chrome://sandbox"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		clean, cc := context.WithTimeout(context.Background(), time.Second)
		defer cc()
		if e := page.Context(clean).Close(); e != nil {
			t.Error(e)
		}
	}()
	if e = waitDOM(ctx, page); e != nil {
		t.Fatal(e)
	}
	obj, e := page.Eval(`() => document.body.innerText`)
	if e != nil {
		t.Fatal(e)
	}
	status := strings.Join(strings.Fields(obj.Value.Str()), " ")
	t.Log("sandbox status:", status)
	if runtime.GOOS == "linux" && (!(strings.Contains(status, "Namespace sandbox Yes") || strings.Contains(status, "Layer 1 Sandbox Namespace")) || !strings.Contains(status, "Seccomp-BPF sandbox Yes")) {
		t.Fatal("Chromium sandbox not enabled:", status)
	}
}

func TestBrowserConcurrency(t *testing.T) {
	b, s, srv := testBrowser(t)
	r := DefaultRequest()
	r.URL = srv.URL + "/dynamic"
	selector := "#never"
	r.WaitForSelector = &selector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := s.Extract(ctx, r); done <- e }()
	}
	deadline := time.Now().Add(time.Second)
	active := false
	for time.Now().Before(deadline) {
		contexts, e := proto.TargetGetBrowserContexts{}.Call(browserRoot(b))
		if e == nil && len(contexts.BrowserContextIDs) == 2 && len(s.slots) == 2 {
			active = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !active {
		t.Fatal("two isolated contexts not active")
	}
	request := httptest.NewRequest("POST", "/extract", strings.NewReader(fmt.Sprintf(`{"url":%q}`, r.URL)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	Handler{s, s.cfg, quietLog()}.ServeHTTP(recorder, request)
	if recorder.Code != 429 || recorder.Header().Get("Retry-After") != "1" {
		t.Fatal(recorder.Code, recorder.Body)
	}
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case e := <-done:
			if e == nil {
				t.Fatal("cancelled request succeeded")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("parallel cleanup stalled")
		}
	}
	contexts, e := proto.TargetGetBrowserContexts{}.Call(browserRoot(b))
	if e != nil || len(contexts.BrowserContextIDs) != 0 || len(s.slots) != 0 {
		t.Fatal("parallel resource leak", contexts, e)
	}
	r.WaitForSelector = nil
	if _, e = s.Extract(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	t.Log("two concurrent contexts, 429 saturation and cleanup verified")
}
func TestBrowserRequestStateIsolation(t *testing.T) {
	b, s, srv := testBrowser(t)
	r := DefaultRequest()
	r.URL = srv.URL + "/dynamic"
	for i := 0; i < 3; i++ {
		if _, e := s.Extract(context.Background(), r); e != nil {
			t.Fatal(e)
		}
	}
	if browserRoot(b).LoadState("", &proto.TargetCreateBrowserContext{}) {
		t.Fatal("request state retained by application controller")
	}
	b.mu.RLock()
	conn := b.connection
	b.mu.RUnlock()
	deadline := time.Now().Add(time.Second)
	for conn.events.Len() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if conn.events.Len() != 1 {
		t.Fatal("request event subscriptions remain:", conn.events.Len())
	}
	t.Log("request Rod state is isolated; only the application event subscription remains")
}

func TestBrowserAuxiliaryShellWaitsForContent(t *testing.T) {
	_, s, srv := testBrowser(t)
	r := DefaultRequest()
	r.URL = srv.URL + "/shell-delayed"
	out, e := s.Extract(context.Background(), r)
	if e != nil || !strings.Contains(out.Content, "DELAYEDSEARCHRESULT") {
		t.Fatal(out, e)
	}
	r.URL = srv.URL + "/shell-only"
	_, e = s.Extract(context.Background(), r)
	expectCode(t, e, "no_content")
}
