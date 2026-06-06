// Command hopperd is the Hopper control plane: a job queue + worker registry +
// artifact store that dispatches docker jobs to pull-based worker nodes.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mcpeixoto/hopper/internal/blob"
	"github.com/mcpeixoto/hopper/internal/config"
	gh "github.com/mcpeixoto/hopper/internal/github"
	"github.com/mcpeixoto/hopper/internal/handler"
	"github.com/mcpeixoto/hopper/internal/reaper"
	"github.com/mcpeixoto/hopper/internal/store"
	"github.com/mcpeixoto/hopper/internal/updater"
	"github.com/mcpeixoto/hopper/internal/version"
)

func setupLogging(level, format string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if strings.ToLower(format) == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func main() {
	cfg := config.Load()
	setupLogging(cfg.LogLevel, cfg.LogFormat)
	slog.Info("hopperd starting", "version", version.Version, "port", cfg.Port)

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	if cfg.OperatorToken == "" || cfg.NodeToken == "" {
		slog.Warn("auth disabled (dev mode): HOPPER_OPERATOR_TOKEN / HOPPER_NODE_TOKEN unset")
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

	// Optional GitHub Actions runner integration.
	if cfg.GitHubToken != "" {
		api.GitHub = &handler.GitHubRunner{
			Client:        gh.NewClient(cfg.GitHubToken),
			WebhookSecret: cfg.GitHubWebhookSecret,
			RunnerImage:   cfg.RunnerImage,
			TriggerLabels: cfg.RunnerTriggerLabels,
			JobLabels:     cfg.RunnerJobLabels,
		}
		slog.Info("github actions runner integration enabled", "image", cfg.RunnerImage, "trigger", cfg.RunnerTriggerLabels)
		if cfg.GitHubWebhookSecret == "" {
			slog.Warn("HOPPER_GITHUB_WEBHOOK_SECRET unset — webhook signature verification disabled")
		}
	}

	// Background reaper: requeue expired-lease jobs, mark stale/dead workers.
	stopReaper := make(chan struct{})
	go reaper.Run(db, time.Duration(cfg.LeaseSeconds)*time.Second, stopReaper)
	defer close(stopReaper)

	// Optional retention: purge old terminal jobs + their orphaned blobs.
	if cfg.RetentionDays > 0 {
		stopRetention := make(chan struct{})
		go reaper.RunRetention(db, blobs, cfg.RetentionDays, stopRetention)
		defer close(stopRetention)
		slog.Info("job retention enabled", "days", cfg.RetentionDays)
	}

	// Opt-in self-update: poll GitHub releases and re-exec on a newer version.
	if cfg.AutoUpdate {
		up := updater.New(version.Repo, "hopperd", version.Version)
		go up.Run(context.Background(), time.Duration(cfg.UpdateIntervalM)*time.Minute)
		slog.Info("self-update enabled", "interval_min", cfg.UpdateIntervalM)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.RouterWithLimit(api, cfg.OperatorToken, cfg.NodeToken, cfg.CORSOrigins, cfg.SubmitRPM),
		ReadHeaderTimeout: 5 * time.Second,
		// No WriteTimeout: /api/jobs/claim is a long-poll that intentionally
		// holds the connection open; per-request deadlines guard it instead.
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		slog.Info("hopperd listening", "addr", ":"+cfg.Port, "db", cfg.DBPath)
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
