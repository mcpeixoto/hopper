// Package config loads Hopper's runtime configuration from HOPPER_* environment
// variables. There is no config file — env-first, with sensible dev defaults.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the control-plane (hopperd) settings. Worker-agent settings live
// in [AgentConfig].
type Config struct {
	Port            string   // HTTP listen port
	DBPath          string   // SQLite file path
	ArtifactDir     string   // directory for input/output/log blobs
	OperatorToken   string   // bearer token for submit/admin routes ("" = auth disabled)
	NodeToken       string   // bearer token for worker-plane routes ("" = auth disabled)
	CORSOrigins     []string // allowed browser origins for the GUI
	LeaseSeconds    int      // visibility timeout granted on claim
	LongPollSeconds int      // how long /api/jobs/claim blocks waiting for work
	AutoUpdate      bool     // self-update to the latest release when enabled
	UpdateIntervalM int      // minutes between self-update checks
}

// Load reads the control-plane configuration from the environment.
func Load() Config {
	return Config{
		Port:            env("HOPPER_PORT", "8080"),
		DBPath:          env("HOPPER_DB_PATH", "data/hopper.db"),
		ArtifactDir:     env("HOPPER_ARTIFACT_DIR", "data/artifacts"),
		OperatorToken:   env("HOPPER_OPERATOR_TOKEN", ""),
		NodeToken:       env("HOPPER_NODE_TOKEN", ""),
		CORSOrigins:     splitList(env("HOPPER_CORS_ORIGINS", "http://localhost:5173")),
		LeaseSeconds:    envInt("HOPPER_LEASE_SECONDS", 120),
		LongPollSeconds: envInt("HOPPER_LONGPOLL_SECONDS", 25),
		AutoUpdate:      env("HOPPER_AUTOUPDATE", "") != "",
		UpdateIntervalM: envInt("HOPPER_UPDATE_INTERVAL_MIN", 60),
	}
}

// AgentConfig holds the worker-agent (hopper-agent) settings.
type AgentConfig struct {
	ControlURL      string   // base URL of the control plane
	NodeToken       string   // bearer token presented on the worker plane
	Hostname        string   // node hostname reported on register
	Labels          []string // capabilities this node advertises
	PullPolicy      string   // "always" | "if-not-present"
	AllowNet        bool     // pass --network to the job container instead of --network none
	CPULimit        string   // docker --cpus value ("" = unset)
	MemLimit        string   // docker --memory value ("" = unset)
	PollInterval    int      // seconds between claim attempts when idle
	LocalAddr       string   // bind address for the local node GUI / status API
	WorkRoot        string   // base directory for per-job work dirs
	AutoUpdate      bool     // self-update to the latest release when enabled
	UpdateIntervalM int      // minutes between self-update checks
}

// LoadAgent reads the worker-agent configuration from the environment.
func LoadAgent() AgentConfig {
	host, _ := os.Hostname()
	return AgentConfig{
		ControlURL:      env("HOPPER_CONTROL_URL", "http://localhost:8080"),
		NodeToken:       env("HOPPER_NODE_TOKEN", ""),
		Hostname:        env("HOPPER_HOSTNAME", host),
		Labels:          splitList(env("HOPPER_LABELS", "")),
		PullPolicy:      env("HOPPER_PULL_POLICY", "if-not-present"),
		AllowNet:        env("HOPPER_ALLOW_NET", "") != "",
		CPULimit:        env("HOPPER_CPU", ""),
		MemLimit:        env("HOPPER_MEM", ""),
		PollInterval:    envInt("HOPPER_POLL_INTERVAL", 2),
		LocalAddr:       env("HOPPER_AGENT_ADDR", "127.0.0.1:8765"),
		WorkRoot:        env("HOPPER_WORK_ROOT", "data/work"),
		AutoUpdate:      env("HOPPER_AUTOUPDATE", "") != "",
		UpdateIntervalM: envInt("HOPPER_UPDATE_INTERVAL_MIN", 60),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// splitList parses a comma-separated env value into a trimmed, non-empty slice.
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
