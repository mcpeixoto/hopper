// Package client is a Go HTTP client for the Hopper control-plane API. It is used
// by the worker agent (node plane) and by operator tooling (submission plane).
//
// It deliberately defines its own DTOs rather than importing the store package,
// so binaries that only talk to the API (the agent, the CLI) don't compile in the
// SQLite driver.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Job mirrors the control plane's job JSON.
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
	Attempts         int               `json:"attempts"`
	MaxAttempts      int               `json:"max_attempts"`
	TimeoutS         int               `json:"timeout_s"`
	ExitCode         *int              `json:"exit_code,omitempty"`
	Error            string            `json:"error,omitempty"`
	SubmittedBy      string            `json:"submitted_by,omitempty"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	StartedAt        string            `json:"started_at,omitempty"`
	FinishedAt       string            `json:"finished_at,omitempty"`
}

// JobSpec is the body for submitting a job.
type JobSpec struct {
	Image       string            `json:"image"`
	Command     []string          `json:"command,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Labels      []string          `json:"labels,omitempty"`
	Priority    int               `json:"priority,omitempty"`
	TimeoutS    int               `json:"timeout_s,omitempty"`
	MaxAttempts int               `json:"max_attempts,omitempty"`
	SubmittedBy string            `json:"submitted_by,omitempty"`
	Paused      bool              `json:"paused,omitempty"`
}

// Worker mirrors the control plane's worker JSON.
type Worker struct {
	ID            string     `json:"id"`
	Hostname      string     `json:"hostname"`
	Labels        []string   `json:"labels"`
	Status        string     `json:"status"`
	LastHeartbeat string     `json:"last_heartbeat,omitempty"`
	Telemetry     *Telemetry `json:"telemetry,omitempty"`
	RegisteredAt  string     `json:"registered_at"`
}

// Telemetry is a node's reported resource snapshot.
type Telemetry struct {
	Load1      float64 `json:"load1"`
	Load5      float64 `json:"load5"`
	Load15     float64 `json:"load15"`
	CPUs       int     `json:"cpus"`
	MemTotalMB int64   `json:"mem_total_mb"`
	MemFreeMB  int64   `json:"mem_free_mb"`
	DiskFreeMB int64   `json:"disk_free_mb"`
	Slots      int     `json:"slots"`
	Running    int     `json:"running"`
	Images     []struct {
		Repo   string `json:"repo"`
		SizeMB int64  `json:"size_mb"`
	} `json:"images"`
}

// Client talks to a Hopper control plane.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for the control plane at baseURL authenticating with
// token. The HTTP timeout is generous to accommodate long-poll claims.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// --- submission plane (operator) ---

// SubmitJob enqueues a job.
func (c *Client) SubmitJob(ctx context.Context, spec JobSpec) (Job, error) {
	var job Job
	err := c.do(ctx, http.MethodPost, "/api/jobs", spec, &job)
	return job, err
}

// GetJob fetches a job by id.
func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	var job Job
	err := c.do(ctx, http.MethodGet, "/api/jobs/"+id, nil, &job)
	return job, err
}

// ListJobs lists jobs, optionally filtered by status.
func (c *Client) ListJobs(ctx context.Context, status string) ([]Job, error) {
	return c.ListJobsFiltered(ctx, url.Values{"status": {status}})
}

// ListJobsFiltered lists jobs with arbitrary query filters (status, image,
// submitted_by, since, limit).
func (c *Client) ListJobsFiltered(ctx context.Context, q url.Values) ([]Job, error) {
	path := "/api/jobs"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var jobs []Job
	err := c.do(ctx, http.MethodGet, path, nil, &jobs)
	return jobs, err
}

// ListWorkers lists the node fleet (with telemetry).
func (c *Client) ListWorkers(ctx context.Context) ([]Worker, error) {
	var workers []Worker
	err := c.do(ctx, http.MethodGet, "/api/workers", nil, &workers)
	return workers, err
}

// GetWorker returns one node with its telemetry.
func (c *Client) GetWorker(ctx context.Context, id string) (Worker, error) {
	var w Worker
	err := c.do(ctx, http.MethodGet, "/api/workers/"+id, nil, &w)
	return w, err
}

// NodeImages is which images a node has cached (from GET /api/images).
type NodeImages struct {
	WorkerID string   `json:"worker_id"`
	Hostname string   `json:"hostname"`
	Images   []string `json:"images"`
}

// Images reports cached docker images per node, optionally filtered by substring.
func (c *Client) Images(ctx context.Context, filter string) ([]NodeImages, error) {
	path := "/api/images"
	if filter != "" {
		path += "?image=" + filter
	}
	var out []NodeImages
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// CancelJob cancels a job.
func (c *Client) CancelJob(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/jobs/"+id+"/cancel", nil, nil)
}

// ServerVersion returns the control plane's build version (for update convergence).
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/api/version", nil, &out)
	return out.Version, err
}

// ReleaseJob moves a paused job into the queue (after attaching its input).
func (c *Client) ReleaseJob(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/jobs/"+id+"/release", nil, nil)
}

// Schedule mirrors the control plane's schedule JSON.
type Schedule struct {
	ID      string  `json:"id"`
	Name    string  `json:"name,omitempty"`
	Cron    string  `json:"cron"`
	Spec    JobSpec `json:"spec"`
	Enabled bool    `json:"enabled"`
	LastRun string  `json:"last_run,omitempty"`
	NextRun string  `json:"next_run"`
}

// CreateSchedule registers a recurring job (cron + spec).
func (c *Client) CreateSchedule(ctx context.Context, name, cronExpr string, spec JobSpec) (Schedule, error) {
	var sc Schedule
	body := map[string]any{"name": name, "cron": cronExpr, "spec": spec}
	err := c.do(ctx, http.MethodPost, "/api/schedules", body, &sc)
	return sc, err
}

// ListSchedules lists recurring jobs.
func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	var scs []Schedule
	err := c.do(ctx, http.MethodGet, "/api/schedules", nil, &scs)
	return scs, err
}

// DeleteSchedule removes a recurring job.
func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/schedules/"+id, nil, nil)
}

// --- worker plane (node) ---

// RegisterWorker registers this node and returns its worker record.
func (c *Client) RegisterWorker(ctx context.Context, hostname string, labels []string) (Worker, error) {
	var w Worker
	body := map[string]any{"hostname": hostname, "labels": labels}
	err := c.do(ctx, http.MethodPost, "/api/workers/register", body, &w)
	return w, err
}

// Heartbeat refreshes this node's liveness and renews its job leases. If
// telemetry is non-nil it is reported as the node's current resource snapshot.
func (c *Client) Heartbeat(ctx context.Context, workerID string, telemetry any) error {
	var body any
	if telemetry != nil {
		body = map[string]any{"telemetry": telemetry}
	}
	return c.do(ctx, http.MethodPost, "/api/workers/"+workerID+"/heartbeat", body, nil)
}

// ClaimJob long-polls for a job. Returns (nil, nil) when none is available (204).
func (c *Client) ClaimJob(ctx context.Context, workerID string, labels []string) (*Job, error) {
	body := map[string]any{"worker_id": workerID, "labels": labels}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/jobs/claim", body)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errStatus(resp)
	}
	var job Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CompleteJob reports a job's result. status is "done" or "failed".
func (c *Client) CompleteJob(ctx context.Context, id, status string, exitCode *int, outputArtifactID, logsRef, errMsg string) error {
	body := map[string]any{
		"status":             status,
		"exit_code":          exitCode,
		"output_artifact_id": outputArtifactID,
		"logs_ref":           logsRef,
		"error":              errMsg,
	}
	return c.do(ctx, http.MethodPost, "/api/jobs/"+id+"/complete", body, nil)
}

// Artifact mirrors the control plane's artifact JSON.
type Artifact struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
}

// --- artifacts ---

// UploadInput uploads a tar.gz input blob for a job (operator). Returns the artifact.
func (c *Client) UploadInput(ctx context.Context, jobID string, r io.Reader) (Artifact, error) {
	return c.upload(ctx, "/api/jobs/"+jobID+"/input", r)
}

// UploadOutput uploads a tar.gz of the job's output (node). Returns the artifact.
func (c *Client) UploadOutput(ctx context.Context, jobID string, r io.Reader) (Artifact, error) {
	return c.upload(ctx, "/api/jobs/"+jobID+"/output", r)
}

// UploadLogs uploads a job's captured logs (node). Returns the artifact.
func (c *Client) UploadLogs(ctx context.Context, jobID string, r io.Reader) (Artifact, error) {
	return c.upload(ctx, "/api/jobs/"+jobID+"/logs", r)
}

// DownloadInput streams a job's input blob (node). Caller must Close the reader.
func (c *Client) DownloadInput(ctx context.Context, jobID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/jobs/"+jobID+"/input", nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, errStatus(resp)
	}
	return resp.Body, nil
}

// DownloadResult streams a finished job's output blob (operator). Caller closes it.
func (c *Client) DownloadResult(ctx context.Context, jobID string) (io.ReadCloser, error) {
	return c.getStream(ctx, "/api/jobs/"+jobID+"/result")
}

// DownloadLogs streams a job's captured logs (operator). Caller closes it.
func (c *Client) DownloadLogs(ctx context.Context, jobID string) (io.ReadCloser, error) {
	return c.getStream(ctx, "/api/jobs/"+jobID+"/logs")
}

// getStream issues a GET and returns the body on 2xx (caller closes it).
func (c *Client) getStream(ctx context.Context, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, errStatus(resp)
	}
	return resp.Body, nil
}

func (c *Client) upload(ctx context.Context, path string, r io.Reader) (Artifact, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+path, r)
	if err != nil {
		return Artifact{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Artifact{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Artifact{}, errStatus(resp)
	}
	var art Artifact
	if err := json.NewDecoder(resp.Body).Decode(&art); err != nil {
		return Artifact{}, err
	}
	return art, nil
}

// --- internals ---

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errStatus(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func errStatus(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
	return fmt.Errorf("hopper api %s: %s", resp.Status, bytes.TrimSpace(b))
}
