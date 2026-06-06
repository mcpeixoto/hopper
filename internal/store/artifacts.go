package store

import (
	"database/sql"
	"errors"
)

// Artifact is a blob (job input, output, or logs) stored on the control plane.
// The row records metadata; the bytes live on disk under the artifact directory.
type Artifact struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // "input" | "output" | "logs"
	JobID       string `json:"job_id,omitempty"`
	Path        string `json:"path"`
	ContentHash string `json:"content_hash,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

// CreateArtifact records an artifact row and returns it with a generated id.
func (s *Store) CreateArtifact(kind, jobID, path, contentHash string, sizeBytes int64) (Artifact, error) {
	a := Artifact{
		ID:          newID("art"),
		Kind:        kind,
		JobID:       jobID,
		Path:        path,
		ContentHash: contentHash,
		SizeBytes:   sizeBytes,
		CreatedAt:   nowISO(),
	}
	_, err := s.db.Exec(`
		INSERT INTO artifacts (id, kind, job_id, path, content_hash, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Kind, nullable(a.JobID), a.Path, nullable(a.ContentHash), a.SizeBytes, a.CreatedAt)
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// GetArtifact returns the artifact with the given id, or [ErrNotFound].
func (s *Store) GetArtifact(id string) (Artifact, error) {
	var a Artifact
	err := s.db.QueryRow(`
		SELECT id, kind, COALESCE(job_id,''), path, COALESCE(content_hash,''), size_bytes, created_at
		FROM artifacts WHERE id=?`, id).
		Scan(&a.ID, &a.Kind, &a.JobID, &a.Path, &a.ContentHash, &a.SizeBytes, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// LinkInputArtifact attaches an input artifact to a job.
func (s *Store) LinkInputArtifact(jobID, artifactID string) error {
	res, err := s.db.Exec(`UPDATE jobs SET input_artifact_id=?, updated_at=? WHERE id=?`,
		artifactID, nowISO(), jobID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
