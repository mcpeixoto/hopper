package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Worker is a node that claims and runs jobs.
type Worker struct {
	ID            string   `json:"id"`
	Hostname      string   `json:"hostname"`
	Labels        []string `json:"labels"`
	Status        string   `json:"status"`
	LastHeartbeat string   `json:"last_heartbeat,omitempty"`
	RegisteredAt  string   `json:"registered_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// RegisterWorker records (or re-registers, keyed by hostname) a worker and
// returns it. Re-registration refreshes labels and marks the worker online.
func (s *Store) RegisterWorker(hostname string, labels []string) (Worker, error) {
	if hostname == "" {
		return Worker{}, errors.New("hostname is required")
	}
	now := nowISO()
	labels = orEmptySlice(labels)

	existing, err := s.workerByHostname(hostname)
	if err == nil {
		if _, err = s.db.Exec(`
			UPDATE workers SET labels_json=?, status='online', last_heartbeat=?, updated_at=?
			WHERE id=?`, mustJSON(labels), now, now, existing.ID); err != nil {
			return Worker{}, err
		}
		return s.GetWorker(existing.ID)
	}
	if !errors.Is(err, ErrNotFound) {
		return Worker{}, err
	}

	w := Worker{
		ID:            newID("wrk"),
		Hostname:      hostname,
		Labels:        labels,
		Status:        "online",
		LastHeartbeat: now,
		RegisteredAt:  now,
		UpdatedAt:     now,
	}
	if _, err = s.db.Exec(`
		INSERT INTO workers (id, hostname, labels_json, status, last_heartbeat, registered_at, updated_at)
		VALUES (?, ?, ?, 'online', ?, ?, ?)`,
		w.ID, w.Hostname, mustJSON(w.Labels), now, now, now); err != nil {
		return Worker{}, err
	}
	return w, nil
}

// HeartbeatWorker refreshes a worker's liveness timestamp and marks it online.
func (s *Store) HeartbeatWorker(id string) error {
	now := nowISO()
	res, err := s.db.Exec(`
		UPDATE workers SET last_heartbeat=?, status='online', updated_at=? WHERE id=?`,
		now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetWorker returns the worker with the given id, or [ErrNotFound].
func (s *Store) GetWorker(id string) (Worker, error) {
	return scanWorker(s.db.QueryRow(workerSelect+` WHERE id=?`, id))
}

// ListWorkers returns all workers, most recently registered first.
func (s *Store) ListWorkers() ([]Worker, error) {
	rows, err := s.db.Query(workerSelect + ` ORDER BY registered_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Worker
	for rows.Next() {
		w, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkStaleWorkers flags workers whose last heartbeat is older than staleAfter as
// stale, and those older than deadAfter as dead. Returns how many became dead.
func (s *Store) MarkStaleWorkers(staleAfter, deadAfter time.Duration) (int, error) {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	staleCutoff := now.Add(-staleAfter).Format(time.RFC3339)
	deadCutoff := now.Add(-deadAfter).Format(time.RFC3339)

	if _, err := s.db.Exec(`
		UPDATE workers SET status='stale', updated_at=?
		WHERE status='online' AND last_heartbeat < ?`, nowStr, staleCutoff); err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`
		UPDATE workers SET status='dead', updated_at=?
		WHERE status IN ('online','stale') AND last_heartbeat < ?`, nowStr, deadCutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CountWorkersByStatus returns the number of workers in each status.
func (s *Store) CountWorkersByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM workers GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

const workerSelect = `
	SELECT id, hostname, labels_json, status, COALESCE(last_heartbeat,''),
	    registered_at, updated_at
	FROM workers`

func (s *Store) workerByHostname(hostname string) (Worker, error) {
	return scanWorker(s.db.QueryRow(workerSelect+` WHERE hostname=?`, hostname))
}

func scanWorker(row rowScanner) (Worker, error) {
	var w Worker
	var labelsJSON string
	err := row.Scan(&w.ID, &w.Hostname, &labelsJSON, &w.Status, &w.LastHeartbeat,
		&w.RegisteredAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Worker{}, ErrNotFound
	}
	if err != nil {
		return Worker{}, err
	}
	_ = json.Unmarshal([]byte(labelsJSON), &w.Labels)
	w.Labels = orEmptySlice(w.Labels)
	return w, nil
}
