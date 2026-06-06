// Package scheduler fires recurring (cron) jobs. On each tick it finds schedules
// whose next run is due, enqueues a job from the stored spec, and computes the
// following run time from the cron expression.
package scheduler

import (
	"log"
	"time"

	"github.com/mcpeixoto/hopper/internal/cron"
	"github.com/mcpeixoto/hopper/internal/store"
)

// Store is the persistence subset the scheduler needs.
type Store interface {
	DueSchedules(nowISO string) ([]store.Schedule, error)
	SubmitJob(spec store.JobSpec) (store.Job, error)
	MarkScheduleFired(id, lastRun, nextRun string) error
}

// Run ticks every 30s firing due schedules until stop is closed.
func Run(s Store, stop <-chan struct{}) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	Tick(s) // fire anything already due at startup
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			Tick(s)
		}
	}
}

// Tick fires all currently-due schedules. Exposed for testing.
func Tick(s Store) {
	now := time.Now().UTC()
	due, err := s.DueSchedules(now.Format(time.RFC3339))
	if err != nil {
		log.Printf("scheduler: due: %v", err)
		return
	}
	for _, sc := range due {
		sched, err := cron.Parse(sc.Cron)
		if err != nil {
			log.Printf("scheduler: schedule %s has invalid cron %q: %v", sc.ID, sc.Cron, err)
			continue
		}
		spec := sc.Spec
		if spec.SubmittedBy == "" {
			spec.SubmittedBy = "schedule:" + sc.ID
		}
		if _, err := s.SubmitJob(spec); err != nil {
			log.Printf("scheduler: submit for %s: %v", sc.ID, err)
			continue
		}
		next := sched.Next(now)
		if err := s.MarkScheduleFired(sc.ID, now.Format(time.RFC3339), next.Format(time.RFC3339)); err != nil {
			log.Printf("scheduler: mark fired %s: %v", sc.ID, err)
		}
		log.Printf("scheduler: fired %s (%s), next %s", sc.ID, sc.Cron, next.Format(time.RFC3339))
	}
}
