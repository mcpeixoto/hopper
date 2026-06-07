package handler

import (
	"errors"
	"io"
	"net/http"
	"time"

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

// AppendLog appends streamed stdout/stderr to a job's live log while it runs.
// Node auth.
func (a *API) AppendLog(w http.ResponseWriter, r *http.Request) {
	if a.LiveLog == nil {
		writeError(w, http.StatusServiceUnavailable, "live logs not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxArtifactBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	if err := a.LiveLog.Append(r.PathValue("id"), body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetLogs streams a job's logs as text. With ?follow=1 it tails the live log until
// the job is terminal. Otherwise it serves the final logs artifact if present,
// else whatever live output exists so far. Operator auth.
func (a *API) GetLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := a.Store.GetJob(id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if r.URL.Query().Get("follow") == "1" && a.LiveLog != nil {
		a.followLogs(w, r, id, job.LogsRef)
		return
	}
	// Final logs artifact wins once the job is done.
	if job.LogsRef != "" {
		a.streamArtifact(w, job.LogsRef, "text/plain; charset=utf-8")
		return
	}
	// Otherwise serve whatever the live log has so far.
	if a.LiveLog != nil && a.LiveLog.Exists(id) {
		rc, err := a.LiveLog.Open(id)
		if err == nil {
			defer rc.Close()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.Copy(w, rc)
			return
		}
	}
	writeError(w, http.StatusNotFound, "no logs")
}

// followLogs tails the live log, flushing new bytes until the job reaches a
// terminal state. Falls back to the final artifact if the live file is gone.
func (a *API) followLogs(w http.ResponseWriter, r *http.Request, id, logsRef string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	flusher, _ := w.(http.Flusher)
	var offset int64
	for {
		if rc, err := a.LiveLog.Open(id); err == nil {
			if sk, ok := rc.(io.Seeker); ok {
				_, _ = sk.Seek(offset, io.SeekStart)
			}
			n, _ := io.Copy(w, rc)
			rc.Close()
			offset += n
			if n > 0 && flusher != nil {
				flusher.Flush()
			}
		}
		job, err := a.Store.GetJob(id)
		if err != nil || job.Status == "done" || job.Status == "failed" || job.Status == "cancelled" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
