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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"  // postgres driver ("postgres")
	_ "modernc.org/sqlite" // sqlite driver ("sqlite"), pure Go
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
                       CHECK (status IN ('paused','queued','in_flight','done','failed','cancelled')),
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
    telemetry_json TEXT NOT NULL DEFAULT '{}',
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

CREATE TABLE IF NOT EXISTS schedules (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    cron       TEXT NOT NULL,
    spec_json  TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    last_run   TEXT,
    next_run   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_schedules_next ON schedules(enabled, next_run);
`

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// database wraps *sql.DB and rewrites `?` placeholders to the driver's dialect,
// so the same query strings work on both SQLite (`?`) and Postgres (`$1`).
type database struct {
	*sql.DB
	rebind func(string) string
}

func (d *database) Exec(q string, a ...any) (sql.Result, error) { return d.DB.Exec(d.rebind(q), a...) }
func (d *database) Query(q string, a ...any) (*sql.Rows, error) { return d.DB.Query(d.rebind(q), a...) }
func (d *database) QueryRow(q string, a ...any) *sql.Row        { return d.DB.QueryRow(d.rebind(q), a...) }

// Store wraps the database connection (SQLite or Postgres).
type Store struct {
	db *database
}

func identity(q string) string { return q }

// toDollar rewrites positional `?` placeholders to Postgres `$1, $2, …`. Hopper's
// queries never contain a literal `?`, so a simple pass is safe.
func toDollar(q string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteString("$")
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

// isPostgres reports whether a DSN targets Postgres.
func isPostgres(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// Open opens the database at path/DSN and applies the schema. A `postgres://` (or
// `postgresql://`) DSN uses Postgres; anything else is a SQLite file path
// (":memory:" for an ephemeral test database).
func Open(path string) (*Store, error) {
	var sqldb *sql.DB
	var rebind func(string) string
	var err error

	if isPostgres(path) {
		if sqldb, err = sql.Open("postgres", path); err != nil {
			return nil, err
		}
		sqldb.SetMaxOpenConns(10)
		rebind = toDollar
	} else {
		// Ensure the parent directory exists for a file-backed database.
		if path != ":memory:" && path != "" {
			if dir := filepath.Dir(path); dir != "." && dir != "/" {
				if err = os.MkdirAll(dir, 0o755); err != nil {
					return nil, err
				}
			}
		}
		if sqldb, err = sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON"); err != nil {
			return nil, err
		}
		sqldb.SetMaxOpenConns(1) // single writer
		rebind = identity
	}

	db := &database{DB: sqldb, rebind: rebind}
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	s.migrate()
	return s, nil
}

// migrate applies idempotent additive migrations for databases created by an
// earlier schema. Errors (e.g. "duplicate column") are ignored by design.
func (s *Store) migrate() {
	_, _ = s.db.Exec(`ALTER TABLE workers ADD COLUMN telemetry_json TEXT NOT NULL DEFAULT '{}'`)
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
