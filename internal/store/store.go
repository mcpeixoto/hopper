// Package store is Hopper's persistence layer: a SQLite database that doubles as
// the job queue. There is no external broker — the jobs table IS the queue.
//
// SQLite runs with WAL journalling and a single writer connection
// (SetMaxOpenConns(1)), which serializes writes and keeps the atomic
// claim/complete transitions race-free without extra locking.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
    id                 TEXT PRIMARY KEY,
    image              TEXT NOT NULL,
    command_json       TEXT NOT NULL DEFAULT '[]',
    env_json           TEXT NOT NULL DEFAULT '{}',
    labels_json        TEXT NOT NULL DEFAULT '[]',
    priority           INTEGER NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'queued'
                       CHECK (status IN ('queued','in_flight','done','failed','cancelled')),
    input_artifact_id  TEXT,
    output_artifact_id TEXT,
    claimed_by         TEXT,
    lease_expires_at   TEXT,
    attempts           INTEGER NOT NULL DEFAULT 0,
    max_attempts       INTEGER NOT NULL DEFAULT 3,
    timeout_s          INTEGER NOT NULL DEFAULT 3600,
    exit_code          INTEGER,
    logs_ref           TEXT,
    error              TEXT,
    submitted_by       TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    started_at         TEXT,
    finished_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(status, priority, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_lease ON jobs(status, lease_expires_at);

CREATE TABLE IF NOT EXISTS workers (
    id             TEXT PRIMARY KEY,
    hostname       TEXT NOT NULL,
    labels_json    TEXT NOT NULL DEFAULT '[]',
    status         TEXT NOT NULL DEFAULT 'online'
                   CHECK (status IN ('online','stale','dead','draining')),
    last_heartbeat TEXT,
    registered_at  TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_workers_hostname ON workers(hostname);

CREATE TABLE IF NOT EXISTS artifacts (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    job_id       TEXT,
    path         TEXT NOT NULL,
    content_hash TEXT,
    size_bytes   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);
`

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Pass ":memory:" for an ephemeral test database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// nowISO returns the current UTC time as RFC3339 — the format used for every
// TEXT timestamp column.
func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// newID returns a short random identifier with the given prefix, e.g. "job_a1b2c3d4e5f6".
func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
