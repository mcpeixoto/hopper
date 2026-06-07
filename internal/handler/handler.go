// Package handler implements the control-plane HTTP API: job submission and
// results for operators, and the claim/complete worker plane for node agents.
package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mcpeixoto/hopper/internal/blob"
	"github.com/mcpeixoto/hopper/internal/metrics"
	"github.com/mcpeixoto/hopper/internal/notify"
	"github.com/mcpeixoto/hopper/internal/store"
)

// maxBodyBytes caps JSON request bodies. Artifact blobs use their own streaming
// endpoints, so job/worker JSON is always small.
const maxBodyBytes = 1 << 20

// API holds the dependencies shared by every handler.
type API struct {
	Store           *store.Store
	Blob            *blob.Store      // nil disables artifact endpoints
	GitHub          *GitHubRunner    // nil disables the GitHub Actions integration
	Notifier        *notify.Notifier // nil disables completion webhooks
	LeaseSeconds    int
	LongPollSeconds int
}

// fireIfTerminal sends a completion webhook if the job is now in a terminal state.
func (a *API) fireIfTerminal(jobID string) {
	if a.Notifier == nil {
		return
	}
	j, err := a.Store.GetJob(jobID)
	if err != nil {
		return
	}
	switch j.Status {
	case "done", "failed", "cancelled":
		a.Notifier.Fire(notify.Event{
			Event: "job." + j.Status, JobID: j.ID, Status: j.Status, Image: j.Image,
			ExitCode: j.ExitCode, Error: j.Error, Attempts: j.Attempts,
			SubmittedBy: j.SubmittedBy, FinishedAt: j.FinishedAt,
		})
	}
}

// Health reports liveness. No auth.
func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

// SubmitJob enqueues a new job. Operator auth.
func (a *API) SubmitJob(w http.ResponseWriter, r *http.Request) {
	var spec store.JobSpec
	if !readJSON(w, r, &spec) {
		return
	}
	job, err := a.Store.SubmitJob(spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	metrics.JobSubmitted()
	writeJSON(w, http.StatusCreated, job)
}

// Metrics exposes Prometheus-format counters and gauges. No auth (bind/proxy it
// privately if the numbers are sensitive).
func (a *API) Metrics(w http.ResponseWriter, r *http.Request) {
	gauges := map[string]float64{}
	if counts, err := a.Store.CountJobsByStatus(); err == nil {
		for st, n := range counts {
			gauges["hopper_jobs{status=\""+st+"\"}"] = float64(n)
		}
	}
	if counts, err := a.Store.CountWorkersByStatus(); err == nil {
		for st, n := range counts {
			gauges["hopper_workers{status=\""+st+"\"}"] = float64(n)
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	metrics.WritePrometheus(w, gauges)
}

// ListJobs returns jobs, optionally filtered by ?status=. Operator auth.
func (a *API) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.ListJobs(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if jobs == nil {
		jobs = []store.Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// GetJob returns a single job by id. Operator auth.
func (a *API) GetJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.Store.GetJob(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ReleaseJob moves a paused job into the queue (after its input was attached).
// Operator auth.
func (a *API) ReleaseJob(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.ReleaseJob(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no paused job with that id")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// CancelJob marks a job cancelled. Operator auth.
func (a *API) CancelJob(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.CompleteJob(r.PathValue("id"), "cancelled", nil, "", "", "cancelled by operator"); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.fireIfTerminal(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// claimRequest is the body a worker sends to long-poll for work.
type claimRequest struct {
	WorkerID string   `json:"worker_id"`
	Labels   []string `json:"labels"`
}

// ClaimJob is a long-poll: it blocks up to LongPollSeconds waiting for a
// claimable job, returning 200 with the job or 204 if none appears. Node auth.
func (a *API) ClaimJob(w http.ResponseWriter, r *http.Request) {
	var req claimRequest
	if !readJSON(w, r, &req) {
		return
	}
	deadline := time.Now().Add(time.Duration(a.LongPollSeconds) * time.Second)
	for {
		job, err := a.Store.ClaimJob(req.WorkerID, req.Labels, a.LeaseSeconds)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if job != nil {
			metrics.JobClaimed()
			writeJSON(w, http.StatusOK, job)
			return
		}
		if time.Now().After(deadline) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusNoContent)
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// completeRequest is the body a worker sends when a job finishes.
type completeRequest struct {
	Status           string `json:"status"` // "done" | "failed"
	ExitCode         *int   `json:"exit_code"`
	OutputArtifactID string `json:"output_artifact_id"`
	LogsRef          string `json:"logs_ref"`
	Error            string `json:"error"`
}

// CompleteJob records a job's result. Node auth.
func (a *API) CompleteJob(w http.ResponseWriter, r *http.Request) {
	var req completeRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Status == "" {
		req.Status = "done"
	}
	if err := a.Store.CompleteJob(r.PathValue("id"), req.Status, req.ExitCode, req.OutputArtifactID, req.LogsRef, req.Error); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "job not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	metrics.JobCompleted(req.Status)
	a.fireIfTerminal(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// registerRequest is the body a worker sends to register itself.
type registerRequest struct {
	Hostname string   `json:"hostname"`
	Labels   []string `json:"labels"`
}

// RegisterWorker registers (or re-registers) a node. Node auth.
func (a *API) RegisterWorker(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !readJSON(w, r, &req) {
		return
	}
	worker, err := a.Store.RegisterWorker(req.Hostname, req.Labels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, worker)
}

// heartbeatRequest optionally carries the node's latest telemetry snapshot.
type heartbeatRequest struct {
	Telemetry json.RawMessage `json:"telemetry"`
}

// Heartbeat refreshes worker liveness and renews its job leases. Node auth.
func (a *API) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req heartbeatRequest
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req) // body optional
	tel := ""
	if len(req.Telemetry) > 0 {
		tel = string(req.Telemetry)
	}
	if err := a.Store.HeartbeatWorker(id, tel); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "worker not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.Store.RenewLease(id, a.LeaseSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ListWorkers returns the node fleet (with telemetry). Operator auth.
func (a *API) ListWorkers(w http.ResponseWriter, r *http.Request) {
	workers, err := a.Store.ListWorkers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if workers == nil {
		workers = []store.Worker{}
	}
	writeJSON(w, http.StatusOK, workers)
}

// GetWorker returns a single node with its telemetry. Operator auth.
func (a *API) GetWorker(w http.ResponseWriter, r *http.Request) {
	worker, err := a.Store.GetWorker(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "worker not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, worker)
}

// Images reports which nodes have which cached docker images. With ?image=substr
// it returns only nodes whose cache contains a matching image. Operator auth.
func (a *API) Images(w http.ResponseWriter, r *http.Request) {
	workers, err := a.Store.ListWorkers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	filter := r.URL.Query().Get("image")
	type nodeImages struct {
		WorkerID string   `json:"worker_id"`
		Hostname string   `json:"hostname"`
		Images   []string `json:"images"`
	}
	out := []nodeImages{}
	for _, wk := range workers {
		var tel struct {
			Images []struct {
				Repo string `json:"repo"`
			} `json:"images"`
		}
		if len(wk.Telemetry) > 0 {
			_ = json.Unmarshal(wk.Telemetry, &tel)
		}
		var imgs []string
		for _, im := range tel.Images {
			if filter == "" || strings.Contains(im.Repo, filter) {
				imgs = append(imgs, im.Repo)
			}
		}
		if filter != "" && len(imgs) == 0 {
			continue
		}
		out = append(out, nodeImages{WorkerID: wk.ID, Hostname: wk.Hostname, Images: imgs})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// readJSON decodes the request body into v, writing an error response and
// returning false on failure.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON")
		log.Printf("readJSON: %v", err)
		return false
	}
	return true
}
