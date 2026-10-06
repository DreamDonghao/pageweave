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

	"github.com/DreamDonghao/pageweave/internal/admin"
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

	c, e = admin.LoadSettings(c)
	if e != nil {
		return e
	}
	manager, token, e := admin.New(c)
	if e != nil {
		return e
	}
	// The startup token is deliberately shown once to the local operator, never in HTTP URLs.
	fmt.Fprintln(os.Stdout, "PageWeave admin token: "+token)
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	for {
		if signalCtx.Err() != nil {
			return nil
		}
		updated, loadErr := admin.LoadSettings(c)
		if loadErr != nil {
			return loadErr
		}
		c = updated
		restart, serveErr := serveCycle(signalCtx, c, manager)
		if serveErr != nil {
			return serveErr
		}
		if !restart {
			return nil
		}
	}
}

func serveCycle(signalCtx context.Context, c config.Config, manager *admin.Manager) (restart bool, result error) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: c.LogLevel}))
	manager.SetRuntime(c, "starting", nil)
	browser, e := extraction.NewBrowser(context.Background(), c, network.Policy{}, log)
	if e != nil {
		return false, e
	}
	defer browser.Close()
	service := extraction.NewService(c, browser, network.Policy{}, log)
	srv := server.NewWithAdmin(c, service, log, manager)
	listen, e := net.Listen("tcp", c.Address())
	if e != nil {
		return false, e
	}
	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(listen) }()
	manager.SetRuntime(c, "running", service.Ready)
	log.Info("server_started", "address", c.Address(), "version", buildinfo.Version())
	select {
	case <-signalCtx.Done():
	case <-manager.ApplyRequested():
		restart = true
	case result = <-browser.Fatal():
	case result = <-errs:
		if result == http.ErrServerClosed {
			result = nil
		}
	}
	manager.SetRuntime(c, "restarting", nil)
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
	log.Info("server_stopped", "reload", restart)
	if signalCtx.Err() != nil {
		restart = false
	}
	return restart, result
}
