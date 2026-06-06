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
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
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
}

// Worker mirrors the control plane's worker JSON.
type Worker struct {
	ID            string   `json:"id"`
	Hostname      string   `json:"hostname"`
	Labels        []string `json:"labels"`
	Status        string   `json:"status"`
	LastHeartbeat string   `json:"last_heartbeat,omitempty"`
	RegisteredAt  string   `json:"registered_at"`
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
	path := "/api/jobs"
	if status != "" {
		path += "?status=" + status
	}
	var jobs []Job
	err := c.do(ctx, http.MethodGet, path, nil, &jobs)
	return jobs, err
}

// ListWorkers lists the node fleet.
func (c *Client) ListWorkers(ctx context.Context) ([]Worker, error) {
	var workers []Worker
	err := c.do(ctx, http.MethodGet, "/api/workers", nil, &workers)
	return workers, err
}

// CancelJob cancels a job.
func (c *Client) CancelJob(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/jobs/"+id+"/cancel", nil, nil)
}

// --- worker plane (node) ---

// RegisterWorker registers this node and returns its worker record.
func (c *Client) RegisterWorker(ctx context.Context, hostname string, labels []string) (Worker, error) {
	var w Worker
	body := map[string]any{"hostname": hostname, "labels": labels}
	err := c.do(ctx, http.MethodPost, "/api/workers/register", body, &w)
	return w, err
}

// Heartbeat refreshes this node's liveness and renews its job leases.
func (c *Client) Heartbeat(ctx context.Context, workerID string) error {
	return c.do(ctx, http.MethodPost, "/api/workers/"+workerID+"/heartbeat", nil, nil)
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
func (c *Client) CompleteJob(ctx context.Context, id, status string, exitCode *int, logsRef, errMsg string) error {
	body := map[string]any{"status": status, "exit_code": exitCode, "logs_ref": logsRef, "error": errMsg}
	return c.do(ctx, http.MethodPost, "/api/jobs/"+id+"/complete", body, nil)
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
