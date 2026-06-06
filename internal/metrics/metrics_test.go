package metrics

import (
	"strings"
	"testing"
)

func TestCountersAndPrometheus(t *testing.T) {
	JobSubmitted()
	JobSubmitted()
	JobClaimed()
	JobCompleted("done")
	JobCompleted("failed")

	c := Counters()
	if c["hopper_jobs_submitted_total"] < 2 {
		t.Fatalf("submitted=%d", c["hopper_jobs_submitted_total"])
	}
	if c["hopper_jobs_failed_total"] < 1 {
		t.Fatalf("failed=%d", c["hopper_jobs_failed_total"])
	}

	var sb strings.Builder
	WritePrometheus(&sb, map[string]float64{`hopper_jobs{status="queued"}`: 3})
	out := sb.String()
	if !strings.Contains(out, "hopper_jobs_submitted_total") {
		t.Fatal("missing counter in output")
	}
	if !strings.Contains(out, `hopper_jobs{status="queued"} 3`) {
		t.Fatalf("missing gauge in output:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE hopper_jobs_submitted_total counter") {
		t.Fatal("missing TYPE line")
	}
}
