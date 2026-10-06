package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DreamDonghao/pageweave/internal/buildinfo"
	"github.com/DreamDonghao/pageweave/internal/config"
	"github.com/DreamDonghao/pageweave/internal/extraction"
	"github.com/DreamDonghao/pageweave/internal/network"
	"github.com/DreamDonghao/pageweave/internal/server"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("pageweave %s commit=%s\n", buildinfo.Version(), buildinfo.Commit)
		return nil
	}
	c, e := config.Load()
	if e != nil {
		return e
	}
	if len(os.Args) > 1 {
		if os.Args[1] == "healthcheck" {
			host := c.Host
			if host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			client := http.Client{Timeout: 2 * time.Second}
			res, e := client.Get("http://" + net.JoinHostPort(host, strconv.Itoa(c.Port)) + "/health/ready")
			if e != nil {
				return e
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				return fmt.Errorf("not ready: HTTP %d", res.StatusCode)
			}
			return nil
		}
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogLevel}))
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// Browser lifetime survives the drain period after a signal.
	browser, e := extraction.NewBrowser(context.Background(), c, network.Policy{}, log)
	if e != nil {
		return e
	}
	defer browser.Close()
	service := extraction.NewService(c, browser, network.Policy{}, log)
	srv := server.New(c, service, log)
	listen, e := net.Listen("tcp", c.Address())
	if e != nil {
		return e
	}
	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(listen) }()
	log.Info("server_started", "address", c.Address(), "version", buildinfo.Version())
	var result error
	select {
	case <-signalCtx.Done():
	case result = <-browser.Fatal():
	case result = <-errs:
		if result == http.ErrServerClosed {
			result = nil
		}
	}
	service.StopAccepting()
	browser.StopAccepting()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	drained := make(chan struct{})
	go func() { service.Shutdown(ctx); close(drained) }()
	if e = srv.Shutdown(ctx); e != nil {
		log.Warn("http_shutdown", "error", e)
		_ = srv.Close()
	}
	<-drained
	log.Info("server_stopped")
	return result
}
