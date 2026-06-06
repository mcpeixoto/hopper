package agent

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mcpeixoto/hopper/internal/config"
	"github.com/mcpeixoto/hopper/internal/handler"
	"github.com/mcpeixoto/hopper/internal/runner"
	"github.com/mcpeixoto/hopper/internal/store"
)

// fakeRunner records the specs it is asked to run and returns a canned result.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []runner.Spec
	result runner.Result
	err    error
}

func (f *fakeRunner) Run(ctx context.Context, spec runner.Spec) (runner.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, spec)
	f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestAgentClaimsRunsCompletes(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer db.Close()

	api := &handler.API{Store: db, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(handler.Router(api, "", "", nil))
	defer srv.Close()

	// Queue a job directly in the store.
	job, _ := db.SubmitJob(store.JobSpec{Image: "alpine", Command: []string{"echo", "hi"}})

	fr := &fakeRunner{result: runner.Result{ExitCode: 0, Logs: "hi\n"}}
	ag := New(config.AgentConfig{
		ControlURL:   srv.URL,
		Hostname:     "test-node",
		PollInterval: 1,
	}, t.TempDir())
	ag.Runner = fr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)

	// Wait for the job to reach done.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := db.GetJob(job.ID)
		if got.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not complete; status=%s runner_calls=%d", got.Status, fr.callCount())
		}
		time.Sleep(20 * time.Millisecond)
	}

	if fr.callCount() != 1 {
		t.Fatalf("expected runner called once, got %d", fr.callCount())
	}
	if got := fr.calls[0].Image; got != "alpine" {
		t.Fatalf("runner got wrong image: %q", got)
	}
	if st := ag.Status(); st.JobsDone != 1 || st.WorkerID == "" {
		t.Fatalf("status not updated: %#v", st)
	}
}

// blockingRunner signals when a run starts and blocks until released, so a test
// can observe how many jobs run concurrently.
type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingRunner) Run(ctx context.Context, spec runner.Spec) (runner.Result, error) {
	b.started <- struct{}{}
	<-b.release
	return runner.Result{ExitCode: 0}, nil
}

func TestAgentRunsJobsConcurrently(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	api := &handler.API{Store: db, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(handler.Router(api, "", "", nil))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		db.SubmitJob(store.JobSpec{Image: "alpine"})
	}

	br := &blockingRunner{started: make(chan struct{}, 3), release: make(chan struct{})}
	ag := New(config.AgentConfig{ControlURL: srv.URL, Hostname: "n", PollInterval: 1, Concurrency: 3}, t.TempDir())
	ag.Runner = br

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)

	// All three slots should pick up a job and enter Run concurrently.
	timeout := time.After(5 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case <-br.started:
		case <-timeout:
			t.Fatalf("only %d/3 jobs started concurrently", i)
		}
	}
	if st := ag.Status(); st.Slots != 3 || len(st.Running) != 3 {
		t.Fatalf("want 3 slots and 3 running, got slots=%d running=%d", st.Slots, len(st.Running))
	}
	close(br.release) // let them all finish
}

func TestAgentMarksFailedOnNonZeroExit(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	api := &handler.API{Store: db, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(handler.Router(api, "", "", nil))
	defer srv.Close()

	job, _ := db.SubmitJob(store.JobSpec{Image: "boom", MaxAttempts: 1})

	fr := &fakeRunner{result: runner.Result{ExitCode: 1, Logs: "boom\n"}}
	ag := New(config.AgentConfig{ControlURL: srv.URL, Hostname: "n", PollInterval: 1}, t.TempDir())
	ag.Runner = fr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ag.Run(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := db.GetJob(job.ID)
		if got.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not fail; status=%s", got.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
