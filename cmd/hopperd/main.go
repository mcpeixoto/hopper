// Command hopperd is the Hopper control plane: a job queue + worker registry +
// artifact store that dispatches docker jobs to pull-based worker nodes.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcpeixoto/hopper/internal/blob"
	"github.com/mcpeixoto/hopper/internal/config"
	"github.com/mcpeixoto/hopper/internal/handler"
	"github.com/mcpeixoto/hopper/internal/reaper"
	"github.com/mcpeixoto/hopper/internal/store"
	"github.com/mcpeixoto/hopper/internal/updater"
	"github.com/mcpeixoto/hopper/internal/version"
)

func main() {
	cfg := config.Load()
	log.Printf("hopperd %s starting", version.Version)

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	if cfg.OperatorToken == "" || cfg.NodeToken == "" {
		log.Printf("WARNING: HOPPER_OPERATOR_TOKEN / HOPPER_NODE_TOKEN unset — auth disabled (dev mode)")
	}

	blobs, err := blob.New(cfg.ArtifactDir)
	if err != nil {
		log.Fatalf("blob store: %v", err)
	}

	api := &handler.API{
		Store:           db,
		Blob:            blobs,
		LeaseSeconds:    cfg.LeaseSeconds,
		LongPollSeconds: cfg.LongPollSeconds,
	}

	// Background reaper: requeue expired-lease jobs, mark stale/dead workers.
	stopReaper := make(chan struct{})
	go reaper.Run(db, time.Duration(cfg.LeaseSeconds)*time.Second, stopReaper)
	defer close(stopReaper)

	// Opt-in self-update: poll GitHub releases and re-exec on a newer version.
	if cfg.AutoUpdate {
		up := updater.New(version.Repo, "hopperd", version.Version)
		go up.Run(context.Background(), time.Duration(cfg.UpdateIntervalM)*time.Minute)
		log.Printf("self-update enabled (every %dm)", cfg.UpdateIntervalM)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Router(api, cfg.OperatorToken, cfg.NodeToken, cfg.CORSOrigins),
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout: /api/jobs/claim is a long-poll that intentionally
		// holds the connection open; per-request deadlines guard it instead.
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		log.Printf("hopperd listening on :%s (db=%s)", cfg.Port, cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
