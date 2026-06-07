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
	"github.com/mcpeixoto/hopper/internal/runner"
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

	// Optional private-registry login so jobs can pull private images.
	if cfg.RegistryAuth && cfg.RegistryUser != "" {
		if err := runner.New("").Login(ctx, cfg.RegistryServer, cfg.RegistryUser, cfg.RegistryPass); err != nil {
			log.Printf("registry login failed: %v", err)
		} else {
			log.Printf("logged in to registry %q as %s", cfg.RegistryServer, cfg.RegistryUser)
		}
	}

	// Opt-in self-update: converge to the CONTROL PLANE's version (so the whole
	// fleet matches the server), not just the latest GitHub release. systemd's
	// Restart=always relaunches the replaced binary after re-exec.
	if cfg.AutoUpdate {
		go ag.ConvergeVersionLoop(ctx, time.Duration(cfg.UpdateIntervalM)*time.Minute)
		log.Printf("self-update enabled — converging to server version every %dm", cfg.UpdateIntervalM)
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
		log.Print("shutdown signal received, draining current job")
		// Give the agent time to report the in-flight job (so it requeues fast)
		// before we exit. ag.Run returns once the current job is reported.
		select {
		case <-errc:
		case <-time.After(45 * time.Second):
			log.Print("drain timed out")
		}
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
