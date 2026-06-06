package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/mcpeixoto/hopper/internal/store"
)

// maxArtifactBytes caps a single uploaded blob (input/output/logs).
const maxArtifactBytes = 1 << 30 // 1 GiB

// storeArtifact saves an uploaded stream as a content-addressed blob and records
// an artifact row.
func (a *API) storeArtifact(kind, jobID string, r io.Reader) (store.Artifact, error) {
	if a.Blob == nil {
		return store.Artifact{}, errors.New("artifact storage not configured")
	}
	path, hash, size, err := a.Blob.Save(r)
	if err != nil {
		return store.Artifact{}, err
	}
	return a.Store.CreateArtifact(kind, jobID, path, hash, size)
}

// streamArtifact writes the blob for artifactID to w.
func (a *API) streamArtifact(w http.ResponseWriter, artifactID, contentType string) {
	if a.Blob == nil {
		writeError(w, http.StatusInternalServerError, "artifact storage not configured")
		return
	}
	art, err := a.Store.GetArtifact(artifactID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rc, err := a.Blob.Open(art.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "blob unavailable")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-SHA256", art.ContentHash)
	_, _ = io.Copy(w, rc)
}

// PutInput stores an input artifact (a tar.gz of the job's /work/in) and links it
// to the job. Operator auth.
func (a *API) PutInput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.Store.GetJob(id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxArtifactBytes)
	art, err := a.storeArtifact("input", id, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.LinkInputArtifact(id, art.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not link input")
		return
	}
	writeJSON(w, http.StatusCreated, art)
}

// GetInput streams a job's input blob. Node auth (the agent downloads it).
func (a *API) GetInput(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.GetJob(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if job.InputArtifactID == "" {
		writeError(w, http.StatusNotFound, "job has no input")
		return
	}
	a.streamArtifact(w, job.InputArtifactID, "application/gzip")
}

// PutOutput stores a job's output artifact (tar.gz of /work/out) and returns it.
// The agent passes the returned id to /complete. Node auth.
func (a *API) PutOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxArtifactBytes)
	art, err := a.storeArtifact("output", id, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, art)
}

// PutLogs stores a job's captured logs as an artifact and returns it. Node auth.
func (a *API) PutLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, maxArtifactBytes)
	art, err := a.storeArtifact("logs", id, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, art)
}

// GetResult streams a finished job's output blob. Operator auth.
func (a *API) GetResult(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.GetJob(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	switch job.Status {
	case "done":
		if job.OutputArtifactID == "" {
			writeError(w, http.StatusNoContent, "job produced no output")
			return
		}
		a.streamArtifact(w, job.OutputArtifactID, "application/gzip")
	case "failed", "cancelled":
		writeError(w, http.StatusConflict, "job "+job.Status)
	default:
		writeError(w, http.StatusAccepted, "job not finished")
	}
}

// GetLogs streams a job's captured logs as text. Operator auth.
func (a *API) GetLogs(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.GetJob(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if job.LogsRef == "" {
		writeError(w, http.StatusNotFound, "no logs")
		return
	}
	a.streamArtifact(w, job.LogsRef, "text/plain; charset=utf-8")
}
