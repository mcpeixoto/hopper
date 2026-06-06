package scheduler

import (
	"testing"
	"time"

	"github.com/mcpeixoto/hopper/internal/store"
)

func TestTickFiresDueSchedule(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// A schedule already due (next_run in the past).
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	sc, err := db.CreateSchedule("nightly", "* * * * *", store.JobSpec{Image: "alpine"}, past)
	if err != nil {
		t.Fatal(err)
	}

	Tick(db)

	// A job should have been enqueued from the spec.
	jobs, _ := db.ListJobs("")
	if len(jobs) != 1 || jobs[0].Image != "alpine" {
		t.Fatalf("expected 1 alpine job, got %d", len(jobs))
	}
	if jobs[0].SubmittedBy != "schedule:"+sc.ID {
		t.Fatalf("submitted_by=%q", jobs[0].SubmittedBy)
	}

	// next_run should have advanced into the future.
	scs, _ := db.ListSchedules()
	if scs[0].NextRun <= past {
		t.Fatalf("next_run not advanced: %s", scs[0].NextRun)
	}

	// Not due again immediately.
	Tick(db)
	jobs, _ = db.ListJobs("")
	if len(jobs) != 1 {
		t.Fatalf("schedule fired twice; got %d jobs", len(jobs))
	}
}

func TestDeleteSchedule(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	sc, _ := db.CreateSchedule("x", "0 * * * *", store.JobSpec{Image: "a"}, time.Now().UTC().Format(time.RFC3339))
	if err := db.DeleteSchedule(sc.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSchedule(sc.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
