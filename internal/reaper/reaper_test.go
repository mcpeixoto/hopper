package reaper

import (
	"errors"
	"testing"
	"time"
)

// fakeStore records calls and returns canned results.
type fakeStore struct {
	requeued, failed int
	requeueErr       error
	dead             int
	markErr          error

	requeueCalls int
	markCalls    int
	gotStale     time.Duration
	gotDead      time.Duration
}

func (f *fakeStore) RequeueExpired() (int, int, error) {
	f.requeueCalls++
	return f.requeued, f.failed, f.requeueErr
}

func (f *fakeStore) MarkStaleWorkers(staleAfter, deadAfter time.Duration) (int, error) {
	f.markCalls++
	f.gotStale, f.gotDead = staleAfter, deadAfter
	return f.dead, f.markErr
}

func TestSweepCallsBoth(t *testing.T) {
	f := &fakeStore{requeued: 2, dead: 1}
	Sweep(f, time.Minute, 3*time.Minute)
	if f.requeueCalls != 1 || f.markCalls != 1 {
		t.Fatalf("expected one call each, got requeue=%d mark=%d", f.requeueCalls, f.markCalls)
	}
	if f.gotStale != time.Minute || f.gotDead != 3*time.Minute {
		t.Fatalf("cutoffs not passed through: stale=%v dead=%v", f.gotStale, f.gotDead)
	}
}

func TestSweepToleratesErrors(t *testing.T) {
	f := &fakeStore{requeueErr: errors.New("boom"), markErr: errors.New("bang")}
	// Should not panic; errors are logged, not propagated.
	Sweep(f, time.Minute, 3*time.Minute)
}

func TestRunStopsOnClose(t *testing.T) {
	f := &fakeStore{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		Run(f, 2*time.Second, stop) // interval clamps to 5s, so no tick fires
		close(done)
	}()
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop on close")
	}
}
