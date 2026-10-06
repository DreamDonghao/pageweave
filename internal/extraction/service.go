package extraction

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/network"
)

// Renderer is the test replacement boundary for browser rendering.
type Renderer interface {
	Render(context.Context, Request) (Snapshot, error)
	Ready() bool
}

// Service owns admission slots and in-flight request cancellation.
type Service struct {
	cfg      config.Config
	renderer Renderer
	policy   network.Policy
	log      *slog.Logger
	slots    chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	stopping bool
	active   sync.WaitGroup
}

// NewService binds extraction to process-wide budgets and the supplied renderer.
func NewService(c config.Config, r Renderer, p network.Policy, log *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{cfg: c, renderer: r, policy: p, log: log, slots: make(chan struct{}, c.MaxConcurrency), ctx: ctx, cancel: cancel}
}

// Ready reports whether new requests may be admitted.
func (s *Service) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.stopping && s.renderer.Ready()
}

// StopAccepting prevents WaitGroup additions before shutdown starts waiting.
func (s *Service) StopAccepting() { s.mu.Lock(); s.stopping = true; s.mu.Unlock() }

// Shutdown drains requests, then cancels them if the drain budget expires.
// Synchronous conversion is bounded but cancellation is cooperative.
func (s *Service) Shutdown(ctx context.Context) {
	s.StopAccepting()
	done := make(chan struct{})
	go func() { s.active.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		s.cancel()
		<-done
	}
	s.cancel()
}

// Extract uses one total budget for DNS, rendering and content conversion.
func (s *Service) Extract(ctx context.Context, r Request) (out Response, err error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	s.mu.Lock()
	if s.stopping || !s.renderer.Ready() {
		s.mu.Unlock()
		return out, problem(503, "browser_unavailable", "浏览器不可用", nil)
	}
	select {
	case s.slots <- struct{}{}:
		s.active.Add(1)
	default:
		s.mu.Unlock()
		return out, problem(429, "too_many_requests", "处理槽位已满", nil)
	}
	s.mu.Unlock()
	defer func() { <-s.slots; s.active.Done() }()
	if e := s.policy.Check(ctx, r.URL); e != nil {
		if ctx.Err() != nil {
			return out, problem(504, "extraction_timeout", "网址检查超时", e)
		}
		return out, problem(403, "blocked_url", "网址被网络策略拒绝", e)
	}
	snap, e := s.renderer.Render(ctx, r)
	if e != nil {
		return out, normalize(e)
	}
	if len(snap.HTML) > s.cfg.MaxHTMLBytes {
		return out, problem(413, "content_too_large", "HTML 快照超过上限", nil)
	}
	out, e = Convert(ctx, snap, r)
	if e != nil {
		return out, normalize(e)
	}
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}
func normalize(e error) error {
	var api *Error
	if errors.As(e, &api) {
		return api
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return problem(504, "extraction_timeout", "网页提取超过服务端时间限制", e)
	}
	return problem(500, "internal_error", "网页提取内部错误", e)
}
