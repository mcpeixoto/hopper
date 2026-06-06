// Package metrics holds lightweight in-process counters exposed in Prometheus
// text format. It avoids a client-library dependency — a handful of atomic
// counters and a small writer are enough for a single-binary control plane.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"sync/atomic"
)

var (
	jobsSubmitted atomic.Int64
	jobsClaimed   atomic.Int64
	jobsCompleted atomic.Int64
	jobsFailed    atomic.Int64
)

// JobSubmitted records a successful submission.
func JobSubmitted() { jobsSubmitted.Add(1) }

// JobClaimed records a successful claim.
func JobClaimed() { jobsClaimed.Add(1) }

// JobCompleted records a terminal completion (status done/failed/cancelled).
func JobCompleted(status string) {
	jobsCompleted.Add(1)
	if status == "failed" {
		jobsFailed.Add(1)
	}
}

// Counters returns a snapshot of the cumulative counters.
func Counters() map[string]int64 {
	return map[string]int64{
		"hopper_jobs_submitted_total": jobsSubmitted.Load(),
		"hopper_jobs_claimed_total":   jobsClaimed.Load(),
		"hopper_jobs_completed_total": jobsCompleted.Load(),
		"hopper_jobs_failed_total":    jobsFailed.Load(),
	}
}

// WritePrometheus writes the counters plus the provided gauges (e.g. queue depth,
// worker counts) in Prometheus text exposition format.
func WritePrometheus(w io.Writer, gauges map[string]float64) {
	counters := Counters()
	keys := make([]string, 0, len(counters))
	for k := range counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", k, k, counters[k])
	}
	gkeys := make([]string, 0, len(gauges))
	for k := range gauges {
		gkeys = append(gkeys, k)
	}
	sort.Strings(gkeys)
	for _, k := range gkeys {
		fmt.Fprintf(w, "# TYPE %s gauge\n%s %g\n", k, k, gauges[k])
	}
}
