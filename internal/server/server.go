package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/extraction"
)

// New configures routes and transport timeouts without starting the listener.
func New(c config.Config, s *extraction.Service, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/extract", extraction.Handler{Service: s, Config: c, Log: log})
	health := func(ready bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id := extraction.RequestID()
			w.Header().Set("X-Request-ID", id)
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET")
				extraction.WriteError(w, id, &extraction.Error{Status: 405, Code: "method_not_allowed", Message: "只支持 GET"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if ready && !s.Ready() {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"status":"unavailable"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}
	mux.HandleFunc("/health/live", health(false))
	mux.HandleFunc("/health/ready", health(true))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		extraction.WriteError(w, extraction.RequestID(), &extraction.Error{Status: 404, Code: "not_found", Message: "接口不存在"})
	})
	return &http.Server{Addr: c.Address(), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: c.RequestTimeout + 10*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
