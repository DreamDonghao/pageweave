package extraction

import (
	"context"
	"testing"
	"time"

	"github.com/DreamDonghao/pageweave/internal/network"
)

func TestConcurrencyAndCancellation(t *testing.T) {
	entered := make(chan struct{})
	c := testConfig(t)
	c.MaxConcurrency = 1
	f := fakeRenderer{render: func(ctx context.Context, r Request) (Snapshot, error) {
		close(entered)
		<-ctx.Done()
		return Snapshot{}, ctx.Err()
	}}
	s := NewService(c, f, network.Policy{FixtureURL: "https://fixture.test"}, quietLog())
	r := DefaultRequest()
	r.URL = "https://fixture.test"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := s.Extract(ctx, r); done <- e }()
	<-entered
	_, e := s.Extract(context.Background(), r)
	api := e.(*Error)
	if api.Code != "too_many_requests" {
		t.Fatal(e)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel stalled")
	}
	if len(s.slots) != 0 {
		t.Fatal("slot leak")
	}
	s.renderer = fakeRenderer{}
	if _, e = s.Extract(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	s.Shutdown(context.Background())
	if s.Ready() {
		t.Fatal("ready after shutdown")
	}
	_, e = s.Extract(context.Background(), r)
	if e.(*Error).Code != "browser_unavailable" {
		t.Fatal(e)
	}
}
func TestShutdownCancelsHandlers(t *testing.T) {
	entered := make(chan struct{})
	s := NewService(testConfig(t), fakeRenderer{render: func(ctx context.Context, r Request) (Snapshot, error) {
		close(entered)
		<-ctx.Done()
		return Snapshot{}, ctx.Err()
	}}, network.Policy{FixtureURL: "https://fixture.test"}, quietLog())
	r := DefaultRequest()
	r.URL = "https://fixture.test"
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.Extract(context.Background(), r) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s.Shutdown(ctx)
	<-done
	if len(s.slots) != 0 {
		t.Fatal("slot leak")
	}
}
