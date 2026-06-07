package store

import "testing"

func TestListJobsFiltered(t *testing.T) {
	s := newTestStore(t)
	s.SubmitJob(JobSpec{Image: "alpine:3.20", SubmittedBy: "ci"})
	s.SubmitJob(JobSpec{Image: "golang:1.22", SubmittedBy: "schedule:x"})
	s.SubmitJob(JobSpec{Image: "alpine:3.19", SubmittedBy: "ci"})

	// Image substring.
	got, _ := s.ListJobsFiltered(JobFilter{Image: "alpine"})
	if len(got) != 2 {
		t.Fatalf("image filter: want 2, got %d", len(got))
	}
	// Submitted-by substring.
	got, _ = s.ListJobsFiltered(JobFilter{SubmittedBy: "schedule"})
	if len(got) != 1 || got[0].Image != "golang:1.22" {
		t.Fatalf("submitted_by filter: %#v", got)
	}
	// Limit.
	got, _ = s.ListJobsFiltered(JobFilter{Limit: 1})
	if len(got) != 1 {
		t.Fatalf("limit: want 1, got %d", len(got))
	}
	// Combined image + submitted_by.
	got, _ = s.ListJobsFiltered(JobFilter{Image: "alpine", SubmittedBy: "ci"})
	if len(got) != 2 {
		t.Fatalf("combined filter: want 2, got %d", len(got))
	}
}
