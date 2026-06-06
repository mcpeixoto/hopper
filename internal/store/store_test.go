package store

import (
	"testing"
	"time"
)

// newTestStore opens an in-memory database. With a single writer connection the
// in-memory DB persists for the lifetime of the Store.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSubmitAndGetJob(t *testing.T) {
	s := newTestStore(t)
	job, err := s.SubmitJob(JobSpec{Image: "alpine", Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if job.Status != "queued" {
		t.Fatalf("want status queued, got %q", job.Status)
	}
	if job.TimeoutS != 3600 || job.MaxAttempts != 3 {
		t.Fatalf("defaults not applied: timeout=%d attempts=%d", job.TimeoutS, job.MaxAttempts)
	}

	got, err := s.GetJob(job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Command) != 2 || got.Command[1] != "hi" {
		t.Fatalf("command round-trip failed: %#v", got.Command)
	}
}

func TestSubmitJobRequiresImage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SubmitJob(JobSpec{}); err == nil {
		t.Fatal("expected error for missing image")
	}
}

func TestGetJobNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetJob("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestClaimLifecycle(t *testing.T) {
	s := newTestStore(t)
	job, _ := s.SubmitJob(JobSpec{Image: "alpine"})

	claimed, err := s.ClaimJob("wrk_1", nil, 60)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a claimed job")
	}
	if claimed.Status != "in_flight" || claimed.ClaimedBy != "wrk_1" || claimed.Attempts != 1 {
		t.Fatalf("bad claim state: %#v", claimed)
	}
	if claimed.LeaseExpiresAt == "" {
		t.Fatal("expected a lease")
	}

	// Queue is now empty.
	if again, _ := s.ClaimJob("wrk_2", nil, 60); again != nil {
		t.Fatal("expected no second claim")
	}

	exit := 0
	if err := s.CompleteJob(job.ID, "done", &exit, "", "", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done, _ := s.GetJob(job.ID)
	if done.Status != "done" || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("bad completion: %#v", done)
	}
}

func TestClaimPriorityOrder(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.SubmitJob(JobSpec{Image: "low", Priority: 0})
	_, _ = s.SubmitJob(JobSpec{Image: "high", Priority: 10})

	claimed, _ := s.ClaimJob("wrk", nil, 60)
	if claimed == nil || claimed.Image != "high" {
		t.Fatalf("expected high-priority job first, got %#v", claimed)
	}
}

func TestClaimLabelMatching(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.SubmitJob(JobSpec{Image: "gpu-job", Labels: []string{"gpu"}})

	// A worker without the label cannot claim it.
	if claimed, _ := s.ClaimJob("cpu-worker", []string{"cpu"}, 60); claimed != nil {
		t.Fatalf("worker without gpu label should not claim, got %#v", claimed)
	}
	// A worker with the label can.
	claimed, _ := s.ClaimJob("gpu-worker", []string{"cpu", "gpu"}, 60)
	if claimed == nil || claimed.Image != "gpu-job" {
		t.Fatalf("gpu worker should claim gpu job, got %#v", claimed)
	}
}

func TestFailedJobRequeuesUntilMaxAttempts(t *testing.T) {
	s := newTestStore(t)
	job, _ := s.SubmitJob(JobSpec{Image: "flaky", MaxAttempts: 2})

	// Attempt 1: claim then fail -> requeued (attempts < max).
	_, _ = s.ClaimJob("wrk", nil, 60)
	if err := s.CompleteJob(job.ID, "failed", nil, "", "", "boom"); err != nil {
		t.Fatalf("complete fail 1: %v", err)
	}
	j, _ := s.GetJob(job.ID)
	if j.Status != "queued" {
		t.Fatalf("want requeued (queued) after attempt 1, got %q", j.Status)
	}

	// Attempt 2: claim then fail -> permanently failed (attempts == max).
	_, _ = s.ClaimJob("wrk", nil, 60)
	if err := s.CompleteJob(job.ID, "failed", nil, "", "", "boom again"); err != nil {
		t.Fatalf("complete fail 2: %v", err)
	}
	j, _ = s.GetJob(job.ID)
	if j.Status != "failed" {
		t.Fatalf("want failed after attempt 2, got %q", j.Status)
	}
}

func TestRequeueExpired(t *testing.T) {
	s := newTestStore(t)
	job, _ := s.SubmitJob(JobSpec{Image: "slow", MaxAttempts: 3})

	// Claim with a lease that is already expired.
	if _, err := s.ClaimJob("dead-worker", nil, -1); err != nil {
		t.Fatalf("claim: %v", err)
	}
	requeued, failed, err := s.RequeueExpired()
	if err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if requeued != 1 || failed != 0 {
		t.Fatalf("want requeued=1 failed=0, got %d %d", requeued, failed)
	}
	j, _ := s.GetJob(job.ID)
	if j.Status != "queued" {
		t.Fatalf("want job back in queue, got %q", j.Status)
	}
}

func TestRenewLease(t *testing.T) {
	s := newTestStore(t)
	job, _ := s.SubmitJob(JobSpec{Image: "x"})
	_, _ = s.ClaimJob("wrk", nil, 1)
	before, _ := s.GetJob(job.ID)

	time.Sleep(1100 * time.Millisecond)
	if err := s.RenewLease("wrk", 120); err != nil {
		t.Fatalf("renew: %v", err)
	}
	after, _ := s.GetJob(job.ID)
	if after.LeaseExpiresAt <= before.LeaseExpiresAt {
		t.Fatalf("lease not extended: before=%s after=%s", before.LeaseExpiresAt, after.LeaseExpiresAt)
	}
}

func TestListJobsFilter(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.SubmitJob(JobSpec{Image: "a"})
	b, _ := s.SubmitJob(JobSpec{Image: "b"})
	_, _ = s.ClaimJob("wrk", nil, 60)

	queued, _ := s.ListJobs("queued")
	inflight, _ := s.ListJobs("in_flight")
	all, _ := s.ListJobs("")
	if len(all) != 2 {
		t.Fatalf("want 2 total, got %d", len(all))
	}
	// Highest priority/oldest claimed first; b was second so a got claimed.
	_ = b
	if len(queued)+len(inflight) != 2 {
		t.Fatalf("filter mismatch: queued=%d inflight=%d", len(queued), len(inflight))
	}
}
