package store

import (
	"testing"
	"time"
)

func TestRegisterWorkerIdempotent(t *testing.T) {
	s := newTestStore(t)
	w1, err := s.RegisterWorker("laptop", []string{"cpu"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Re-register same hostname -> same id, refreshed labels.
	w2, err := s.RegisterWorker("laptop", []string{"cpu", "gpu"})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if w1.ID != w2.ID {
		t.Fatalf("re-register should keep id: %s != %s", w1.ID, w2.ID)
	}
	if len(w2.Labels) != 2 {
		t.Fatalf("labels not refreshed: %#v", w2.Labels)
	}
	all, _ := s.ListWorkers()
	if len(all) != 1 {
		t.Fatalf("want 1 worker, got %d", len(all))
	}
}

func TestHeartbeatUnknownWorker(t *testing.T) {
	s := newTestStore(t)
	if err := s.HeartbeatWorker("ghost", ""); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestHeartbeatStoresTelemetry(t *testing.T) {
	s := newTestStore(t)
	w, _ := s.RegisterWorker("node", nil)
	tel := `{"load1":0.5,"cpus":8,"images":[{"repo":"alpine:3.20","size_mb":7}]}`
	if err := s.HeartbeatWorker(w.ID, tel); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetWorker(w.ID)
	if string(got.Telemetry) != tel {
		t.Fatalf("telemetry not stored: %s", got.Telemetry)
	}
	// An empty-telemetry heartbeat must not wipe the stored telemetry.
	if err := s.HeartbeatWorker(w.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWorker(w.ID)
	if string(got.Telemetry) != tel {
		t.Fatalf("empty heartbeat wiped telemetry: %s", got.Telemetry)
	}
}

func TestMarkStaleWorkers(t *testing.T) {
	s := newTestStore(t)
	w, _ := s.RegisterWorker("node", nil)

	// Force an old heartbeat so the worker looks dead.
	old := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE workers SET last_heartbeat=? WHERE id=?`, old, w.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	dead, err := s.MarkStaleWorkers(time.Minute, 5*time.Minute)
	if err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	if dead != 1 {
		t.Fatalf("want 1 dead, got %d", dead)
	}
	got, _ := s.GetWorker(w.ID)
	if got.Status != "dead" {
		t.Fatalf("want dead, got %q", got.Status)
	}
}
