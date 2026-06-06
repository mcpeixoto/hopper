// Command hopper-agent is the Hopper worker node: it registers with a control
// plane, long-polls for jobs, runs them with docker, and reports results. It also
// serves a local status API (and node GUI) on a loopback address.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcpeixoto/hopper/internal/agent"
	"github.com/mcpeixoto/hopper/internal/config"
	"github.com/mcpeixoto/hopper/internal/updater"
	"github.com/mcpeixoto/hopper/internal/version"
)

func main() {
	cfg := config.LoadAgent()
	log.Printf("hopper-agent %s starting (control=%s)", version.Version, cfg.ControlURL)

	if cfg.Hostname == "" {
		log.Fatal("hostname could not be determined; set HOPPER_HOSTNAME")
	}

	ag := agent.New(cfg, cfg.WorkRoot)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Opt-in self-update: poll GitHub releases and re-exec on a newer version.
	// systemd's Restart=always also relaunches the replaced binary after re-exec.
	if cfg.AutoUpdate {
		up := updater.New(version.Repo, "hopper-agent", version.Version)
		go up.Run(ctx, time.Duration(cfg.UpdateIntervalM)*time.Minute)
		log.Printf("self-update enabled (every %dm)", cfg.UpdateIntervalM)
	}

	// Local status server for the node GUI (loopback).
	statusSrv := &http.Server{
		Addr:              cfg.LocalAddr,
		Handler:           ag.StatusHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("node status API on http://%s", cfg.LocalAddr)
		if err := statusSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("status server: %v", err)
		}
	}()

	// Run the worker loop until a signal arrives.
	errc := make(chan error, 1)
	go func() { errc <- ag.Run(ctx) }()

	select {
	case <-ctx.Done():
		log.Print("shutdown signal received, draining")
	case err := <-errc:
		if err != nil && err != context.Canceled {
			log.Printf("agent stopped: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = statusSrv.Shutdown(shutdownCtx)
	os.Exit(0)
}
