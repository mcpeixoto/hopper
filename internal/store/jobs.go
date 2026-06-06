package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Job is a unit of work: a docker image + command to run on some node.
type Job struct {
	ID               string            `json:"id"`
	Image            string            `json:"image"`
	Command          []string          `json:"command"`
	Env              map[string]string `json:"env"`
	Labels           []string          `json:"labels"`
	Priority         int               `json:"priority"`
	Status           string            `json:"status"`
	InputArtifactID  string            `json:"input_artifact_id,omitempty"`
	OutputArtifactID string            `json:"output_artifact_id,omitempty"`
	ClaimedBy        string            `json:"claimed_by,omitempty"`
	LeaseExpiresAt   string            `json:"lease_expires_at,omitempty"`
	Attempts         int               `json:"attempts"`
	MaxAttempts      int               `json:"max_attempts"`
	TimeoutS         int               `json:"timeout_s"`
	ExitCode         *int              `json:"exit_code,omitempty"`
	LogsRef          string            `json:"logs_ref,omitempty"`
	Error            string            `json:"error,omitempty"`
	SubmittedBy      string            `json:"submitted_by,omitempty"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	StartedAt        string            `json:"started_at,omitempty"`
	FinishedAt       string            `json:"finished_at,omitempty"`
}

// JobSpec describes a job to submit. Zero values get sensible defaults.
type JobSpec struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env"`
	Labels      []string          `json:"labels"`
	Priority    int               `json:"priority"`
	TimeoutS    int               `json:"timeout_s"`
	MaxAttempts int               `json:"max_attempts"`
	SubmittedBy string            `json:"submitted_by"`
	// Paused creates the job in the 'paused' state so an input artifact can be
	// attached before it becomes claimable. Release it with ReleaseJob.
	Paused bool `json:"paused"`
}

// Validation limits for a submitted job.
const (
	maxImageLen   = 512
	maxCommandLen = 4096
	maxEnvVars    = 256
	maxLabels     = 64
)

// SubmitJob inserts a new queued job and returns it.
func (s *Store) SubmitJob(spec JobSpec) (Job, error) {
	if spec.Image == "" {
		return Job{}, errors.New("image is required")
	}
	if len(spec.Image) > maxImageLen {
		return Job{}, errors.New("image name too long")
	}
	if len(spec.Command) > maxCommandLen {
		return Job{}, errors.New("command has too many arguments")
	}
	if len(spec.Env) > maxEnvVars {
		return Job{}, errors.New("too many env vars")
	}
	if len(spec.Labels) > maxLabels {
		return Job{}, errors.New("too many labels")
	}
	if spec.TimeoutS <= 0 {
		spec.TimeoutS = 3600
	}
	if spec.MaxAttempts <= 0 {
		spec.MaxAttempts = 3
	}
	now := nowISO()
	status := "queued"
	if spec.Paused {
		status = "paused"
	}
	j := Job{
		ID:          newID("job"),
		Image:       spec.Image,
		Command:     orEmptySlice(spec.Command),
		Env:         orEmptyMap(spec.Env),
		Labels:      orEmptySlice(spec.Labels),
		Priority:    spec.Priority,
		Status:      status,
		MaxAttempts: spec.MaxAttempts,
		TimeoutS:    spec.TimeoutS,
		SubmittedBy: spec.SubmittedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(`
		INSERT INTO jobs (id, image, command_json, env_json, labels_json, priority,
		    status, max_attempts, timeout_s, submitted_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Image, mustJSON(j.Command), mustJSON(j.Env), mustJSON(j.Labels),
		j.Priority, status, j.MaxAttempts, j.TimeoutS, j.SubmittedBy, j.CreatedAt, j.UpdatedAt)
	if err != nil {
		return Job{}, err
	}
	return j, nil
}

// CountJobsByStatus returns the number of jobs in each status.
func (s *Store) CountJobsByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM jobs GROUP BY status`)
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

// ReleaseJob moves a paused job into the queue so workers can claim it. Used
// after attaching an input artifact.
func (s *Store) ReleaseJob(id string) error {
	res, err := s.db.Exec(`UPDATE jobs SET status='queued', updated_at=? WHERE id=? AND status='paused'`,
		nowISO(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetJob returns the job with the given id, or [ErrNotFound].
func (s *Store) GetJob(id string) (Job, error) {
	return scanJob(s.db.QueryRow(jobSelect+` WHERE id = ?`, id))
}

// ListJobs returns jobs, newest first. If status is non-empty it filters by it.
func (s *Store) ListJobs(status string) ([]Job, error) {
	q := jobSelect
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClaimJob atomically claims the highest-priority queued job whose required
// labels are all satisfied by the worker's labels, marking it in_flight with a
// lease. Returns (nil, nil) when no claimable job exists.
//
// With a single writer connection this select-then-update is race-free; the
// UPDATE additionally guards on status='queued' so a job is never double-claimed.
func (s *Store) ClaimJob(workerID string, workerLabels []string, leaseSeconds int) (*Job, error) {
	rows, err := s.db.Query(jobSelect + ` WHERE status = 'queued'
		ORDER BY priority DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	var candidates []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, j)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	have := make(map[string]bool, len(workerLabels))
	for _, l := range workerLabels {
		have[l] = true
	}
	for _, j := range candidates {
		if !labelsSatisfied(j.Labels, have) {
			continue
		}
		now := time.Now().UTC()
		lease := now.Add(time.Duration(leaseSeconds) * time.Second).Format(time.RFC3339)
		res, err := s.db.Exec(`
			UPDATE jobs SET status='in_flight', claimed_by=?, lease_expires_at=?,
			    attempts=attempts+1, started_at=COALESCE(started_at, ?), updated_at=?
			WHERE id=? AND status='queued'`,
			workerID, lease, now.Format(time.RFC3339), now.Format(time.RFC3339), j.ID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			claimed, err := s.GetJob(j.ID)
			if err != nil {
				return nil, err
			}
			return &claimed, nil
		}
		// Lost the race for this job; try the next candidate.
	}
	return nil, nil
}

// CompleteJob marks a job done/failed/cancelled and records its result. A failed
// job that still has attempts remaining is returned to the queue instead.
func (s *Store) CompleteJob(id, status string, exitCode *int, outputArtifactID, logsRef, errMsg string) error {
	if status != "done" && status != "failed" && status != "cancelled" {
		return errors.New("invalid completion status: " + status)
	}
	now := nowISO()

	if status == "failed" {
		j, err := s.GetJob(id)
		if err != nil {
			return err
		}
		if j.Attempts < j.MaxAttempts {
			_, err := s.db.Exec(`
				UPDATE jobs SET status='queued', claimed_by=NULL, lease_expires_at=NULL,
				    logs_ref=?, error=?, updated_at=? WHERE id=?`,
				nullable(logsRef), nullable(errMsg), now, id)
			return err
		}
	}

	res, err := s.db.Exec(`
		UPDATE jobs SET status=?, exit_code=?, output_artifact_id=?, logs_ref=?,
		    error=?, claimed_by=NULL, lease_expires_at=NULL, finished_at=?, updated_at=?
		WHERE id=?`,
		status, exitCode, nullable(outputArtifactID), nullable(logsRef),
		nullable(errMsg), now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RenewLease extends the lease on every in_flight job claimed by workerID.
func (s *Store) RenewLease(workerID string, leaseSeconds int) error {
	lease := time.Now().UTC().Add(time.Duration(leaseSeconds) * time.Second).Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE jobs SET lease_expires_at=?, updated_at=?
		WHERE status='in_flight' AND claimed_by=?`, lease, nowISO(), workerID)
	return err
}

// RequeueExpired returns to the queue any in_flight job whose lease has expired,
// failing it permanently once it is out of attempts. Returns counts of each.
func (s *Store) RequeueExpired() (requeued, failed int, err error) {
	now := nowISO()
	rows, err := s.db.Query(jobSelect+` WHERE status='in_flight' AND lease_expires_at < ?`, now)
	if err != nil {
		return 0, 0, err
	}
	var expired []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return 0, 0, err
		}
		expired = append(expired, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	for _, j := range expired {
		if j.Attempts >= j.MaxAttempts {
			if _, e := s.db.Exec(`
				UPDATE jobs SET status='failed', error='lease expired (max attempts reached)',
				    claimed_by=NULL, lease_expires_at=NULL, finished_at=?, updated_at=? WHERE id=?`,
				now, now, j.ID); e != nil {
				return requeued, failed, e
			}
			failed++
		} else {
			if _, e := s.db.Exec(`
				UPDATE jobs SET status='queued', claimed_by=NULL, lease_expires_at=NULL,
				    error='lease expired, requeued', updated_at=? WHERE id=?`, now, j.ID); e != nil {
				return requeued, failed, e
			}
			requeued++
		}
	}
	return requeued, failed, nil
}

// --- helpers ---

const jobSelect = `
	SELECT id, image, command_json, env_json, labels_json, priority, status,
	    COALESCE(input_artifact_id,''), COALESCE(output_artifact_id,''),
	    COALESCE(claimed_by,''), COALESCE(lease_expires_at,''), attempts, max_attempts,
	    timeout_s, exit_code, COALESCE(logs_ref,''), COALESCE(error,''),
	    COALESCE(submitted_by,''), created_at, updated_at,
	    COALESCE(started_at,''), COALESCE(finished_at,'')
	FROM jobs`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var j Job
	var cmdJSON, envJSON, labelsJSON string
	var exit sql.NullInt64
	err := row.Scan(&j.ID, &j.Image, &cmdJSON, &envJSON, &labelsJSON, &j.Priority,
		&j.Status, &j.InputArtifactID, &j.OutputArtifactID, &j.ClaimedBy,
		&j.LeaseExpiresAt, &j.Attempts, &j.MaxAttempts, &j.TimeoutS, &exit,
		&j.LogsRef, &j.Error, &j.SubmittedBy, &j.CreatedAt, &j.UpdatedAt,
		&j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	_ = json.Unmarshal([]byte(cmdJSON), &j.Command)
	_ = json.Unmarshal([]byte(envJSON), &j.Env)
	_ = json.Unmarshal([]byte(labelsJSON), &j.Labels)
	j.Command = orEmptySlice(j.Command)
	j.Env = orEmptyMap(j.Env)
	j.Labels = orEmptySlice(j.Labels)
	if exit.Valid {
		v := int(exit.Int64)
		j.ExitCode = &v
	}
	return j, nil
}

// labelsSatisfied reports whether every required label is present in have.
func labelsSatisfied(required []string, have map[string]bool) bool {
	for _, r := range required {
		if !have[r] {
			return false
		}
	}
	return true
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
