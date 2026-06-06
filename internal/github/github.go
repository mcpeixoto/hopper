// Package github implements the glue for running GitHub Actions jobs on the Hopper
// fleet: verifying webhook signatures, parsing workflow_job events, and minting
// just-in-time runner registration tokens.
//
// The model is "ephemeral self-hosted runners without Kubernetes": GitHub sends a
// workflow_job:queued webhook, Hopper mints a registration token and submits a
// normal Hopper job that runs the official runner image in --ephemeral mode. A
// free node claims it, the runner executes exactly one workflow run, then exits.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client mints runner registration tokens via the GitHub REST API using a
// personal access token (or GitHub App installation token) with repo admin rights.
type Client struct {
	Token string // GitHub token with `repo` (admin:repo_hook / actions) scope
	API   string // base URL, defaults to https://api.github.com
	HTTP  *http.Client
}

// NewClient returns a GitHub client.
func NewClient(token string) *Client {
	return &Client{Token: token, API: "https://api.github.com", HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// RegistrationToken mints a short-lived runner registration token for a repo
// ("owner/name"). The token is valid for ~1 hour and is single-use per runner.
func (c *Client) RegistrationToken(ctx context.Context, repoFullName string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/runners/registration-token", c.base(), repoFullName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return "", fmt.Errorf("github registration-token: %s: %s", resp.Status, b)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("github registration-token: empty token")
	}
	return out.Token, nil
}

func (c *Client) base() string {
	if c.API == "" {
		return "https://api.github.com"
	}
	return c.API
}

func (c *Client) client() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

// VerifySignature checks a GitHub webhook's X-Hub-Signature-256 header (HMAC-SHA256
// of the body with the shared secret). An empty secret disables verification and
// returns true (development only — log a warning).
func VerifySignature(secret string, body []byte, sigHeader string) bool {
	if secret == "" {
		return true
	}
	const prefix = "sha256="
	if len(sigHeader) <= len(prefix) || sigHeader[:len(prefix)] != prefix {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(sigHeader[len(prefix):])
	if err != nil {
		return false
	}
	return hmac.Equal(want, got)
}

// WorkflowJobEvent is the subset of the workflow_job webhook payload we use.
type WorkflowJobEvent struct {
	Action      string `json:"action"`
	WorkflowJob struct {
		ID     int64    `json:"id"`
		Name   string   `json:"name"`
		Labels []string `json:"labels"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

// ParseWorkflowJob decodes a workflow_job event body.
func ParseWorkflowJob(body []byte) (WorkflowJobEvent, error) {
	var e WorkflowJobEvent
	err := json.Unmarshal(body, &e)
	return e, err
}

// HasAllLabels reports whether labels contains every required label.
func HasAllLabels(labels, required []string) bool {
	have := make(map[string]bool, len(labels))
	for _, l := range labels {
		have[l] = true
	}
	for _, r := range required {
		if !have[r] {
			return false
		}
	}
	return true
}
