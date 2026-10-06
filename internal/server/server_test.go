package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/extraction"
	"github.com/DreamDonghao/pageweave/internal/network"
)

type renderer struct{ ready bool }

func (r *renderer) Ready() bool { return r.ready }
func (r *renderer) Render(context.Context, extraction.Request) (extraction.Snapshot, error) {
	panic("health check must not render")
}
func TestHealthAndRoutes(t *testing.T) {
	c, e := config.Load()
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := &renderer{true}
	s := extraction.NewService(c, r, network.Policy{}, log)
	defer s.Shutdown(context.Background())
	srv := New(c, s, log)
	for _, tt := range []struct {
		path, method string
		status       int
	}{{"/health/live", "GET", 200}, {"/health/ready", "GET", 200}, {"/health/live", "POST", 405}, {"/missing", "GET", 404}, {"/extract/other", "POST", 404}} {
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status {
			t.Fatal(tt, w.Code, w.Body)
		}
	}
	r.ready = false
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
