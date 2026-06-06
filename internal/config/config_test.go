package config

import (
	"reflect"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HOPPER_PORT", "")
	t.Setenv("HOPPER_DB_PATH", "")
	cfg := Load()
	if cfg.Port != "8080" {
		t.Fatalf("default port: %q", cfg.Port)
	}
	if cfg.DBPath != "data/hopper.db" {
		t.Fatalf("default db path: %q", cfg.DBPath)
	}
	if cfg.LeaseSeconds != 120 || cfg.LongPollSeconds != 25 {
		t.Fatalf("default lease/longpoll: %d/%d", cfg.LeaseSeconds, cfg.LongPollSeconds)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("HOPPER_PORT", "9999")
	t.Setenv("HOPPER_LEASE_SECONDS", "30")
	t.Setenv("HOPPER_CORS_ORIGINS", "https://a.com, https://b.com ,")
	cfg := Load()
	if cfg.Port != "9999" || cfg.LeaseSeconds != 30 {
		t.Fatalf("overrides not applied: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.CORSOrigins, []string{"https://a.com", "https://b.com"}) {
		t.Fatalf("cors parse failed: %#v", cfg.CORSOrigins)
	}
}

func TestLoadAgentDefaults(t *testing.T) {
	t.Setenv("HOPPER_CONTROL_URL", "")
	t.Setenv("HOPPER_LABELS", "cpu, gpu")
	a := LoadAgent()
	if a.ControlURL != "http://localhost:8080" {
		t.Fatalf("default control url: %q", a.ControlURL)
	}
	if a.PullPolicy != "if-not-present" {
		t.Fatalf("default pull policy: %q", a.PullPolicy)
	}
	if !reflect.DeepEqual(a.Labels, []string{"cpu", "gpu"}) {
		t.Fatalf("labels parse: %#v", a.Labels)
	}
}
