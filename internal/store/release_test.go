package store

import "testing"

func TestPausedJobNotClaimable(t *testing.T) {
	s := newTestStore(t)
	job, err := s.SubmitJob(JobSpec{Image: "alpine", Paused: true})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if job.Status != "paused" {
		t.Fatalf("want paused, got %q", job.Status)
	}
	// A paused job must not be claimable.
	if c, _ := s.ClaimJob("w", nil, 60); c != nil {
		t.Fatal("paused job should not be claimable")
	}
	// Release it, then it can be claimed.
	if err := s.ReleaseJob(job.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	c, _ := s.ClaimJob("w", nil, 60)
	if c == nil || c.ID != job.ID {
		t.Fatal("released job should be claimable")
	}
}

func TestReleaseUnknownOrNonPaused(t *testing.T) {
	s := newTestStore(t)
	if err := s.ReleaseJob("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound for unknown, got %v", err)
	}
	job, _ := s.SubmitJob(JobSpec{Image: "x"}) // queued, not paused
	if err := s.ReleaseJob(job.ID); err != ErrNotFound {
		t.Fatalf("releasing a non-paused job should be a no-op ErrNotFound, got %v", err)
	}
}
