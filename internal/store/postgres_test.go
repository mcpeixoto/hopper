package store

import (
	"os"
	"testing"
)

// TestPostgresLifecycle runs the core store flow against a real Postgres, gated on
// HOPPER_TEST_PG (a postgres:// DSN). It is skipped when unset, so CI without a
// Postgres service is unaffected.
//
//	HOPPER_TEST_PG=postgres://postgres:pass@localhost:5432/postgres?sslmode=disable \
//	  go test -run TestPostgres ./internal/store/
func TestPostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("HOPPER_TEST_PG")
	if dsn == "" {
		t.Skip("set HOPPER_TEST_PG to a postgres DSN to run")
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer s.Close()

	// Clean slate (idempotent across reruns).
	for _, tbl := range []string{"artifacts", "schedules", "workers", "jobs"} {
		if _, err := s.db.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}

	// submit -> claim -> complete
	job, err := s.SubmitJob(JobSpec{Image: "alpine", Command: []string{"echo", "hi"}, Labels: []string{"cpu"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	claimed, err := s.ClaimJob("wrk1", []string{"cpu"}, 60)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	exit := 0
	if err := s.CompleteJob(job.ID, "done", &exit, "", "", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, _ := s.GetJob(job.ID)
	if got.Status != "done" {
		t.Fatalf("want done, got %q", got.Status)
	}

	// workers + telemetry + counts (exercises LIKE, GROUP BY, json columns)
	w, _ := s.RegisterWorker("pg-node", []string{"cpu"})
	if err := s.HeartbeatWorker(w.ID, `{"cpus":4}`); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if wk, _ := s.GetWorker(w.ID); string(wk.Telemetry) != `{"cpus":4}` {
		t.Fatalf("telemetry: %s", wk.Telemetry)
	}
	if jobs, _ := s.ListJobsFiltered(JobFilter{Image: "alp", Limit: 10}); len(jobs) != 1 {
		t.Fatalf("filtered list: %d", len(jobs))
	}
	if counts, _ := s.CountJobsByStatus(); counts["done"] != 1 {
		t.Fatalf("counts: %#v", counts)
	}
}
