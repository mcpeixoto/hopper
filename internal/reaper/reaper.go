// Package reaper runs the control plane's background maintenance: it returns
// jobs whose lease expired (a node died mid-job) to the queue, and marks workers
// that stopped heartbeating as stale/dead. This is the visibility-timeout pattern
// that makes the pull queue resilient to node failure without any broker.
package reaper

import (
	"log"
	"time"
)

// Store is the subset of the persistence layer the reaper needs.
type Store interface {
	RequeueExpired() (requeued, failed int, err error)
	MarkStaleWorkers(staleAfter, deadAfter time.Duration) (int, error)
}

// Run sweeps the store every lease/2 (min 5s) until stop is closed. The stale and
// dead cutoffs are derived from the lease duration.
func Run(s Store, lease time.Duration, stop <-chan struct{}) {
	interval := lease / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	staleAfter := lease
	deadAfter := 3 * lease

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			Sweep(s, staleAfter, deadAfter)
		}
	}
}

// Sweep performs a single maintenance pass. Exposed for testing.
func Sweep(s Store, staleAfter, deadAfter time.Duration) {
	if requeued, failed, err := s.RequeueExpired(); err != nil {
		log.Printf("reaper: requeue expired: %v", err)
	} else if requeued > 0 || failed > 0 {
		log.Printf("reaper: requeued=%d failed=%d (expired leases)", requeued, failed)
	}
	if dead, err := s.MarkStaleWorkers(staleAfter, deadAfter); err != nil {
		log.Printf("reaper: mark stale workers: %v", err)
	} else if dead > 0 {
		log.Printf("reaper: marked %d worker(s) dead", dead)
	}
}
