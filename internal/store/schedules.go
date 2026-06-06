package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Schedule is a recurring job: a cron expression plus the job spec to enqueue.
type Schedule struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Cron      string  `json:"cron"`
	Spec      JobSpec `json:"spec"`
	Enabled   bool    `json:"enabled"`
	LastRun   string  `json:"last_run,omitempty"`
	NextRun   string  `json:"next_run"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// CreateSchedule stores a recurring job with its first computed next_run.
func (s *Store) CreateSchedule(name, cron string, spec JobSpec, nextRun string) (Schedule, error) {
	sc := Schedule{
		ID: newID("sch"), Name: name, Cron: cron, Spec: spec, Enabled: true,
		NextRun: nextRun, CreatedAt: nowISO(), UpdatedAt: nowISO(),
	}
	_, err := s.db.Exec(`
		INSERT INTO schedules (id, name, cron, spec_json, enabled, next_run, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?)`,
		sc.ID, nullable(name), cron, mustJSON(spec), nextRun, sc.CreatedAt, sc.UpdatedAt)
	if err != nil {
		return Schedule{}, err
	}
	return sc, nil
}

// ListSchedules returns all schedules, newest first.
func (s *Store) ListSchedules() ([]Schedule, error) {
	rows, err := s.db.Query(scheduleSelect + ` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// DeleteSchedule removes a schedule.
func (s *Store) DeleteSchedule(id string) error {
	res, err := s.db.Exec(`DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DueSchedules returns enabled schedules whose next_run is at or before nowISO.
func (s *Store) DueSchedules(nowISO string) ([]Schedule, error) {
	rows, err := s.db.Query(scheduleSelect+` WHERE enabled = 1 AND next_run <= ?`, nowISO)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// MarkScheduleFired records a firing and the next scheduled time.
func (s *Store) MarkScheduleFired(id, lastRun, nextRun string) error {
	_, err := s.db.Exec(`UPDATE schedules SET last_run=?, next_run=?, updated_at=? WHERE id=?`,
		lastRun, nextRun, nowISO(), id)
	return err
}

const scheduleSelect = `
	SELECT id, COALESCE(name,''), cron, spec_json, enabled,
	    COALESCE(last_run,''), next_run, created_at, updated_at
	FROM schedules`

func scanSchedule(row rowScanner) (Schedule, error) {
	var sc Schedule
	var specJSON string
	var enabled int
	err := row.Scan(&sc.ID, &sc.Name, &sc.Cron, &specJSON, &enabled,
		&sc.LastRun, &sc.NextRun, &sc.CreatedAt, &sc.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, err
	}
	_ = json.Unmarshal([]byte(specJSON), &sc.Spec)
	sc.Enabled = enabled != 0
	return sc, nil
}
