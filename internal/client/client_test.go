package client

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/mcpeixoto/hopper/internal/handler"
	"github.com/mcpeixoto/hopper/internal/store"
)

// TestClientAgainstRealServer exercises the client over the real control-plane
// router (auth disabled), covering submit → register → claim → complete.
func TestClientAgainstRealServer(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api := &handler.API{Store: db, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(handler.Router(api, "", "", nil))
	defer srv.Close()

	c := New(srv.URL, "")
	ctx := context.Background()

	job, err := c.SubmitJob(ctx, JobSpec{Image: "alpine", Command: []string{"echo", "hi"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if job.ID == "" || job.Status != "queued" {
		t.Fatalf("bad job: %#v", job)
	}

	w, err := c.RegisterWorker(ctx, "node-1", []string{"cpu"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	claimed, err := c.ClaimJob(ctx, w.ID, []string{"cpu"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claim returned %#v", claimed)
	}

	exit := 0
	if err := c.CompleteJob(ctx, job.ID, "done", &exit, "", "", ""); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, err := c.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("want done, got %q", got.Status)
	}

	// Empty queue → 204 → nil job.
	none, err := c.ClaimJob(ctx, w.ID, []string{"cpu"})
	if err != nil {
		t.Fatalf("claim empty: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil on empty queue, got %#v", none)
	}
}
