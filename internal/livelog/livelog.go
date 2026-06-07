// Package livelog stores a job's stdout/stderr as it runs, so operators can tail
// output before the job finishes. Each job has an append-only file; once the job
// completes the final logs are kept as a content-addressed artifact and the live
// file is removed.
package livelog

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Store manages per-job live log files under a directory.
type Store struct {
	dir string
}

// jobID validation — ids are like "job_<hex>"; keep it strict to avoid traversal.
var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// New creates (if needed) the live-log directory and returns a Store.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("livelog: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(jobID string) (string, error) {
	if !idRe.MatchString(jobID) {
		return "", errors.New("livelog: invalid job id")
	}
	return filepath.Join(s.dir, jobID+".log"), nil
}

// Append adds data to a job's live log (creating it on first write).
func (s *Store) Append(jobID string, data []byte) error {
	p, err := s.path(jobID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// Open opens a job's live log for reading, or returns os.ErrNotExist.
func (s *Store) Open(jobID string) (io.ReadCloser, error) {
	p, err := s.path(jobID)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Exists reports whether a live log file is present for the job.
func (s *Store) Exists(jobID string) bool {
	p, err := s.path(jobID)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Remove deletes a job's live log (no error if absent).
func (s *Store) Remove(jobID string) error {
	p, err := s.path(jobID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
