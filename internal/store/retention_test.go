package store

import (
	"testing"
	"time"
)

func TestPurgeTerminalJobs(t *testing.T) {
	s := newTestStore(t)

	// One done job with an output artifact, finished "long ago".
	done, _ := s.SubmitJob(JobSpec{Image: "a"})
	_, _ = s.ClaimJob("w", nil, 60)
	art, _ := s.CreateArtifact("output", done.ID, "deadbeefhash", "deadbeefhash", 10)
	exit := 0
	_ = s.CompleteJob(done.ID, "done", &exit, art.ID, "", "")
	// Backdate finished_at well past the cutoff.
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE jobs SET finished_at=? WHERE id=?`, old, done.ID); err != nil {
		t.Fatal(err)
	}

	// A second, recent queued job that must survive.
	keep, _ := s.SubmitJob(JobSpec{Image: "b"})

	cutoff := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	orphans, n, err := s.PurgeTerminalJobs(cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 job purged, got %d", n)
	}
	if len(orphans) != 1 || orphans[0] != "deadbeefhash" {
		t.Fatalf("want orphan blob deadbeefhash, got %v", orphans)
	}
	if _, err := s.GetJob(done.ID); err != ErrNotFound {
		t.Fatalf("purged job should be gone, got %v", err)
	}
	if _, err := s.GetJob(keep.ID); err != nil {
		t.Fatalf("recent job should survive: %v", err)
	}
}

func TestPurgeKeepsSharedBlobs(t *testing.T) {
	s := newTestStore(t)
	// Two terminal jobs whose artifacts share the same content hash (dedup).
	for _, img := range []string{"a", "b"} {
		j, _ := s.SubmitJob(JobSpec{Image: img})
		_, _ = s.ClaimJob("w", nil, 60)
		a, _ := s.CreateArtifact("output", j.ID, "sharedhash", "sharedhash", 1)
		exit := 0
		_ = s.CompleteJob(j.ID, "done", &exit, a.ID, "", "")
		old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
		s.db.Exec(`UPDATE jobs SET finished_at=? WHERE id=?`, old, j.ID)
	}
	// Purge only the first by cutoff that catches both — but verify shared blob
	// is only reported orphan once both are gone.
	cutoff := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	orphans, n, err := s.PurgeTerminalJobs(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 purged, got %d", n)
	}
	// After both are deleted the shared blob is unreferenced exactly once.
	count := 0
	for _, p := range orphans {
		if p == "sharedhash" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared blob should be reported orphan once, got %d", count)
	}
}
