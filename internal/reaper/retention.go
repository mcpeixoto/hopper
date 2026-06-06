package reaper

import (
	"log"
	"time"
)

// Purger deletes terminal jobs older than a cutoff and reports orphaned blob paths.
type Purger interface {
	PurgeTerminalJobs(cutoffISO string) (orphanBlobPaths []string, jobsDeleted int, err error)
}

// BlobDeleter removes a stored blob by path.
type BlobDeleter interface {
	Delete(path string) error
}

// RunRetention periodically purges terminal jobs (and their now-orphaned blobs)
// older than `days`, until stop is closed. days <= 0 disables retention.
func RunRetention(p Purger, b BlobDeleter, days int, stop <-chan struct{}) {
	if days <= 0 {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	PurgeOnce(p, b, days) // run once at startup
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			PurgeOnce(p, b, days)
		}
	}
}

// PurgeOnce performs a single retention pass. Exposed for testing.
func PurgeOnce(p Purger, b BlobDeleter, days int) {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	paths, n, err := p.PurgeTerminalJobs(cutoff)
	if err != nil {
		log.Printf("retention: purge: %v", err)
		return
	}
	for _, path := range paths {
		if err := b.Delete(path); err != nil {
			log.Printf("retention: delete blob %s: %v", path, err)
		}
	}
	if n > 0 {
		log.Printf("retention: purged %d job(s), %d blob(s) older than %d days", n, len(paths), days)
	}
}
