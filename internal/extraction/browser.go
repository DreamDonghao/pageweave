package extraction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/network"
)

// Browser owns the shared Chromium process. Only the monitor replaces a failed generation.
type Browser struct {
	cfg            config.Config
	policy         network.Policy
	log            *slog.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	root           *rod.Browser
	connection     *connection
	launch         *launcher.Launcher
	dir            string
	generation     context.Context
	stopGeneration context.CancelFunc
	ready          bool
	done           chan struct{}
	fatal          chan error
}

// NewBrowser starts Chromium without downloading it and supervises recovery.
func NewBrowser(ctx context.Context, c config.Config, p network.Policy, log *slog.Logger) (*Browser, error) {
	ctx, cancel := context.WithCancel(ctx)
	b := &Browser{cfg: c, policy: p, log: log, ctx: ctx, cancel: cancel, done: make(chan struct{}), fatal: make(chan error, 1)}
	if e := b.start(); e != nil {
		cancel()
		return nil, e
	}
	go b.monitor()
	return b, nil
}
func (b *Browser) start() error {
	if _, e := os.Stat(b.cfg.BrowserPath); e != nil {
		return fmt.Errorf("browser path: %w", e)
	}
	dir, e := os.MkdirTemp("", "pageweave-")
	if e != nil {
		return e
	}
	// Rod adds no-sandbox in containers. Remove it explicitly; use /dev/shm and normal site isolation.
	l := launcher.New().Bin(b.cfg.BrowserPath).UserDataDir(dir).NoSandbox(false).Leakless(false)
	l.Delete("disable-popup-blocking").Delete("disable-dev-shm-usage").Delete("disable-site-isolation-trials").Delete("disable-features").Set("remote-debugging-address", "127.0.0.1")
	launchCtx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	url, e := l.Context(launchCtx).Launch()
	cancel()
	if e != nil {
		if l.PID() != 0 {
			l.Kill()
			l.Cleanup()
		}
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			b.log.Warn("browser_temp_cleanup", "error", cleanupErr)
		}
		return fmt.Errorf("launch Chromium: %w", e)
	}
	gen, stop := context.WithCancel(b.ctx)
	connectTimer := time.AfterFunc(5*time.Second, stop)
	conn, connectErr := newConnection(gen, url)
	var root *rod.Browser
	e = connectErr
	if e == nil {
		root = rod.New().Context(gen).Client(conn.forContext(gen))
		e = root.Connect()
	}
	connectTimer.Stop()
	// Connect and its event transport use the generation lifetime, not a request deadline.
	if e == nil {
		root = root.Context(gen)
		checkCtx, checkCancel := context.WithTimeout(gen, 5*time.Second)
		_, e = proto.BrowserGetVersion{}.Call(root.Context(checkCtx))
		checkCancel()
	}
	if e != nil {
		stop()
		if l.PID() != 0 {
			l.Kill()
			l.Cleanup()
		}
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			b.log.Warn("browser_temp_cleanup", "error", cleanupErr)
		}
		return fmt.Errorf("connect Chromium: %w", e)
	}
	b.mu.Lock()
	b.root = root
	b.connection = conn
	b.launch = l
	b.dir = dir
	b.generation = gen
	b.stopGeneration = stop
	b.ready = true
	b.mu.Unlock()
	b.log.Info("browser_ready", "pid", l.PID())
	return nil
}

// Ready reports admission readiness; saturation is tracked by Service.
func (b *Browser) Ready() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ready && b.ctx.Err() == nil
}

// Fatal receives the terminal error when limited recovery is exhausted.
func (b *Browser) Fatal() <-chan error { return b.fatal }

// StopAccepting marks readiness unavailable without interrupting draining requests.
func (b *Browser) StopAccepting() { b.mu.Lock(); b.ready = false; b.mu.Unlock() }
func (b *Browser) dispose() {
	b.mu.Lock()
	b.ready = false
	root, conn, l, dir, stop := b.root, b.connection, b.launch, b.dir, b.stopGeneration
	b.root = nil
	b.connection = nil
	b.launch = nil
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	if root != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		e := root.Context(ctx).Close()
		cancel()
		if e != nil {
			b.log.Warn("browser_close", "error", e)
		}
	}
	if l != nil {
		if l.PID() != 0 {
			l.Kill()
			l.Cleanup()
		}
	}
	if conn != nil {
		select {
		case <-conn.done:
		case <-time.After(2 * time.Second):
			b.log.Warn("browser_event_cleanup_timeout")
		}
	}
	if dir != "" {
		if e := os.RemoveAll(dir); e != nil {
			b.log.Warn("browser_temp_cleanup", "error", e)
		}
	}
}
func (b *Browser) monitor() {
	defer close(b.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-tick.C:
			b.mu.RLock()
			root, ready := b.root, b.ready
			b.mu.RUnlock()
			if !ready || root == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(b.ctx, time.Second)
			_, e := proto.BrowserGetVersion{}.Call(root.Context(ctx))
			cancel()
			if e == nil {
				continue
			}
			b.log.Error("browser_failed", "error", e)
			b.dispose()
			recovered := false
			for attempt := 1; attempt <= 3; attempt++ {
				if b.ctx.Err() != nil {
					return
				}
				if e = b.start(); e == nil {
					recovered = true
					break
				}
				b.log.Error("browser_recovery_failed", "attempt", attempt, "error", e)
				timer := time.NewTimer(time.Duration(attempt) * time.Second)
				select {
				case <-b.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			if !recovered {
				b.fatal <- fmt.Errorf("browser recovery exhausted: %w", e)
				return
			}
		}
	}
}

// Close joins the monitor and releases Chromium and its temporary directory.
func (b *Browser) Close() { b.StopAccepting(); b.cancel(); <-b.done; b.dispose() }

// Snapshot is a bounded HTML capture with its actual document address.
type Snapshot struct {
	HTML, URL, Title string
	Warnings         []string
}

// Render creates an isolated browser context and captures a dynamically rendered page.
// Context cleanup runs with an independent short budget on every return path.
func (b *Browser) Render(ctx context.Context, r Request) (snap Snapshot, err error) {
	b.mu.RLock()
	root, conn, gen, ready := b.root, b.connection, b.generation, b.ready
	b.mu.RUnlock()
	if !ready || root == nil {
		return snap, problem(503, "browser_unavailable", "浏览器不可用", nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(gen, cancel)
	defer stop()
	// Rod records every command in its Browser state map. Use a request-owned
	// controller on the same transport so disposed pages and commands can be collected.
	controller := rod.New().Context(ctx).Client(conn.forContext(ctx))
	if e := controller.Connect(); e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	inc, e := controller.Incognito()
	if e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	defer func() {
		clean, cc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cc()
		if e := inc.Context(clean).Close(); e != nil {
			b.log.Warn("request_context_cleanup", "request_id", ctx.Value(requestIDKey{}), "error", e)
		}
	}()
	page, e := inc.Context(ctx).Page(proto.TargetCreateTarget{URL: "about:blank"})
	if e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	defer func() {
		clean, cc := context.WithTimeout(context.Background(), 2*time.Second)
		defer cc()
		if e := page.Context(clean).Close(); e != nil {
			b.log.Warn("request_page_cleanup", "request_id", ctx.Value(requestIDKey{}), "error", e)
		}
	}()
	frame, e := proto.PageGetFrameTree{}.Call(page)
	if e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	frameID := frame.FrameTree.Frame.ID
	eventsCtx, eventsCancel := context.WithCancel(ctx)
	var stateMu sync.Mutex
	blocked := false
	status := 0
	mime := ""
	var interceptErr error
	wait := page.Context(eventsCtx).EachEvent(func(ev *proto.FetchRequestPaused) {
		policyErr := b.policy.Check(eventsCtx, ev.Request.URL)
		if policyErr == nil && ev.ResponseStatusCode != nil && *ev.ResponseStatusCode >= 300 && *ev.ResponseStatusCode < 400 {
			for _, header := range ev.ResponseHeaders {
				if strings.EqualFold(header.Name, "location") {
					baseURL, baseErr := url.Parse(ev.Request.URL)
					destination, destinationErr := url.Parse(header.Value)
					if baseErr != nil || destinationErr != nil {
						policyErr = fmt.Errorf("invalid redirect URL")
					} else {
						policyErr = b.policy.Check(eventsCtx, baseURL.ResolveReference(destination).String())
					}
					break
				}
			}
		}
		var actionErr error
		if policyErr != nil {
			stateMu.Lock()
			if ev.FrameID == frameID && ev.ResourceType == proto.NetworkResourceTypeDocument {
				blocked = true
			}
			stateMu.Unlock()
			actionErr = proto.FetchFailRequest{RequestID: ev.RequestID, ErrorReason: proto.NetworkErrorReasonBlockedByClient}.Call(page.Context(eventsCtx))
		} else {
			actionErr = proto.FetchContinueRequest{RequestID: ev.RequestID, InterceptResponse: ev.ResponseStatusCode == nil && ev.ResourceType == proto.NetworkResourceTypeDocument}.Call(page.Context(eventsCtx))
		}
		if actionErr != nil && eventsCtx.Err() == nil {
			b.log.Debug("network_intercept_failed", "request_id", ctx.Value(requestIDKey{}), "error", actionErr)
			stateMu.Lock()
			interceptErr = actionErr
			stateMu.Unlock()
			cancel()
		}
	}, func(ev *proto.NetworkResponseReceived) {
		if ev.FrameID == frameID && ev.Type == proto.NetworkResourceTypeDocument {
			stateMu.Lock()
			status = ev.Response.Status
			mime = ev.Response.MIMEType
			stateMu.Unlock()
		}
	})
	eventsDone := make(chan struct{})
	go func() { defer close(eventsDone); wait() }()
	defer func() { eventsCancel(); <-eventsDone }()
	for _, call := range []func() error{
		func() error { return proto.NetworkEnable{}.Call(page) },
		func() error { return proto.NetworkSetBypassServiceWorker{Bypass: true}.Call(page) },
		func() error { return proto.NetworkSetBlockedURLs{Urls: []string{"ws://*", "wss://*"}}.Call(page) },
		func() error {
			return proto.FetchEnable{Patterns: []*proto.FetchRequestPattern{{URLPattern: "*", RequestStage: proto.FetchRequestStageRequest}, {URLPattern: "*", ResourceType: proto.NetworkResourceTypeDocument, RequestStage: proto.FetchRequestStageResponse}}}.Call(page)
		},
	} {
		if e = call(); e != nil {
			return snap, b.browserError(ctx, gen, e)
		}
	}
	// Prevent network paths Fetch does not intercept, including WebSockets, WebRTC and popups.
	_, e = page.EvalOnNewDocument(bootstrapJS)
	if e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	navCtx, nc := context.WithTimeout(ctx, b.cfg.NavigationTimeout)
	e = page.Context(navCtx).Navigate(r.URL)
	if e == nil {
		e = waitDOM(navCtx, page)
	}
	nc()
	stateMu.Lock()
	isBlocked, upStatus, upMime, routeErr := blocked, status, mime, interceptErr
	stateMu.Unlock()
	if isBlocked {
		return snap, problem(403, "blocked_url", "导航目标被网络策略拒绝", nil)
	}
	if routeErr != nil {
		return snap, problem(502, "upstream_error", "网络策略执行失败", routeErr)
	}
	if e != nil {
		if errors.Is(e, context.DeadlineExceeded) {
			return snap, problem(504, "extraction_timeout", "导航超时", e)
		}
		return snap, b.browserError(ctx, gen, e)
	}
	if upStatus >= 400 {
		return snap, problem(502, "upstream_error", fmt.Sprintf("上游 HTTP %d", upStatus), nil)
	}
	if upStatus == 0 || !(upMime == "text/html" || upMime == "application/xhtml+xml") {
		return snap, problem(502, "upstream_error", "上游不是 HTML 文档", nil)
	}
	warnings := []string{}
	e = b.observe(ctx, page, r.WaitForSelector)
	if e != nil {
		if gen.Err() != nil {
			return snap, b.browserError(ctx, gen, e)
		}
		if r.WaitForSelector != nil || ctx.Err() != nil {
			return snap, e
		}
		var apiErr *Error
		if !errors.As(e, &apiErr) || apiErr.Code != "extraction_timeout" {
			return snap, e
		}
		warnings = append(warnings, "render_wait_limit_reached")
	}
	if r.Scroll {
		for step := 0; step < b.cfg.MaxScrollSteps; step++ {
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 500*time.Millisecond {
				break
			}
			prev, e := page.Eval(`() => document.body?.innerText || ''`)
			if e != nil {
				return snap, b.browserError(ctx, gen, e)
			}
			if _, e = page.Eval(`() => window.scrollBy(0,window.innerHeight)`); e != nil {
				return snap, b.browserError(ctx, gen, e)
			}
			if e = b.observe(ctx, page, nil); e != nil {
				if ctx.Err() != nil {
					return snap, e
				}
			}
			next, e := page.Eval(`() => document.body?.innerText || ''`)
			if e != nil {
				return snap, b.browserError(ctx, gen, e)
			}
			if prev.Value.Str() == next.Value.Str() {
				break
			}
			if step+1 == b.cfg.MaxScrollSteps {
				warnings = append(warnings, "scroll_limit_reached")
			}
		}
	}
	obj, e := page.Eval(snapshotJS, b.cfg.MaxHTMLBytes)
	if e != nil {
		return snap, b.browserError(ctx, gen, e)
	}
	if obj.Value.Get("large").Bool() {
		return snap, problem(413, "content_too_large", "HTML 快照超过上限", nil)
	}
	snap = Snapshot{HTML: obj.Value.Get("html").Str(), URL: obj.Value.Get("url").Str(), Title: obj.Value.Get("title").Str(), Warnings: warnings}
	if e = b.policy.Check(ctx, snap.URL); e != nil {
		return snap, problem(403, "blocked_url", "最终地址被网络策略拒绝", e)
	}
	stateMu.Lock()
	isBlocked, upStatus = blocked, status
	stateMu.Unlock()
	if isBlocked {
		return snap, problem(403, "blocked_url", "导航目标被网络策略拒绝", nil)
	}
	if upStatus >= 400 {
		return snap, problem(502, "upstream_error", fmt.Sprintf("上游 HTTP %d", upStatus), nil)
	}
	return snap, nil
}
func (b *Browser) browserError(ctx, gen context.Context, e error) error {
	if gen.Err() != nil {
		return problem(503, "browser_unavailable", "浏览器连接中断", e)
	}
	if ctx.Err() != nil {
		return problem(504, "extraction_timeout", "网页提取超过服务端时间限制", e)
	}
	return problem(502, "upstream_error", "网页加载失败", e)
}
func (b *Browser) observe(ctx context.Context, page *rod.Page, selector *string) error {
	observeCtx, cancel := context.WithTimeout(ctx, b.cfg.RenderWait)
	defer cancel()
	start := time.Now()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	last := ""
	stable := 0
	sel := ""
	if selector != nil {
		sel = *selector
	}
	for {
		select {
		case <-observeCtx.Done():
			return problem(504, "extraction_timeout", "动态内容或指定目标等待超时", observeCtx.Err())
		case <-tick.C:
			obj, e := page.Context(observeCtx).Eval(observeJS, sel, loadingPlaceholders, auxiliaryLabels)
			if e != nil {
				if observeCtx.Err() != nil {
					return problem(504, "extraction_timeout", "动态内容等待超时", e)
				}
				return problem(502, "upstream_error", "动态内容观察失败", e)
			}
			if obj.Value.Get("invalid").Bool() {
				return invalid("wait_for_selector is not valid CSS")
			}
			value := obj.Value.Get("text").Str() + fmt.Sprint(obj.Value.Get("nodes").Int())
			if value == last {
				stable++
			} else {
				stable = 0
				last = value
			}
			if time.Since(start) >= 500*time.Millisecond && stable >= 3 && obj.Value.Get("ready").Bool() && obj.Value.Get("meaningful").Bool() {
				return nil
			}
		}
	}
}

const snapshotJS = `(maxBytes) => {
 const original=document.documentElement;
 if(!original)return {html:'',url:location.href,title:document.title};
 // Bound serialization before transferring it over CDP.
 let estimate=0;const walker=document.createTreeWalker(original,NodeFilter.SHOW_ELEMENT|NodeFilter.SHOW_TEXT|NodeFilter.SHOW_COMMENT);let node=original;
 do {if(node.nodeType===1){estimate+=node.tagName.length*2+5;for(const a of node.attributes)estimate+=a.name.length+a.value.length+4;}else estimate+=node.nodeValue?.length||0;if(estimate>maxBytes)return {large:true};}while(node=walker.nextNode());
 if(new TextEncoder().encode(original.outerHTML).length>maxBytes)return {large:true};
 const copy=original.cloneNode(true),src=[original,...original.querySelectorAll('*')],dst=[copy,...copy.querySelectorAll('*')];
 for(let i=1;i<src.length;i++) {const s=getComputedStyle(src[i]);if(s.display==='none'||s.visibility==='hidden'||s.visibility==='collapse'||src[i].hidden)dst[i].remove();}
 const html=copy.outerHTML;
 return new TextEncoder().encode(html).length>maxBytes?{large:true}:{html,url:location.href,title:document.title};
}`

func waitDOM(ctx context.Context, page *rod.Page) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			obj, e := page.Context(ctx).Eval(`() => document.readyState`)
			if e != nil {
				return e
			}
			if obj.Value.Str() == "interactive" || obj.Value.Str() == "complete" {
				return nil
			}
		}
	}
}

// Install before site scripts run, in each document. Worker realms are deliberately
// unsupported in v1; a native meta policy also restricts network APIs and frame loads.
const bootstrapJS = `{
 const deny=function(){throw new DOMException('Unsupported network API','SecurityError')};
 for(const name of ['WebSocket','Worker','SharedWorker','RTCPeerConnection','webkitRTCPeerConnection']){
  Object.defineProperty(window,name,{value:deny,writable:false,configurable:false});
 }
 if(navigator.serviceWorker){Object.defineProperty(Object.getPrototypeOf(navigator.serviceWorker),'register',{value:()=>Promise.reject(new DOMException('Service workers disabled','SecurityError')),writable:false,configurable:false})}
 Object.defineProperty(window,'open',{value:()=>null,writable:false,configurable:false});
 const install=()=>{if(!document.head)return false;const meta=document.createElement('meta');meta.httpEquiv='Content-Security-Policy';meta.content="worker-src 'none'; connect-src http: https: data: blob:; frame-src 'none'; object-src 'none'";document.head.prepend(meta);return true};
 if(!install()){const observer=new MutationObserver(()=>{if(install())observer.disconnect()});observer.observe(document,{childList:true,subtree:true})}
}`

const observeJS = `(selector,placeholders,auxiliary) => {
 let target=null;
 if(selector){try{target=document.querySelector(selector)}catch(e){return {invalid:true}}}
 const visible=e=>!!e&&e.getClientRects().length>0&&getComputedStyle(e).visibility!=='hidden'&&getComputedStyle(e).display!=='none';
 const area=document.querySelector('main,article,[role=main]')||document.body;
 const text=area?.innerText||'';
 const normalized=text.trim().toLowerCase().replace(/[.。…!！\s]+$/g,'');
 const substantive=auxiliary.reduce((value,label)=>value.split(label).join(''),normalized.replace(/\s+/g,' ')).trim();
 return {meaningful:text.trim().length>0&&!placeholders.includes(normalized)&&substantive.length>0,text,nodes:area?.getElementsByTagName('*').length||0,ready:!selector||(visible(target)&&target.innerText.trim().length>0)};
}`
