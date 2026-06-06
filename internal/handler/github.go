package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/mcpeixoto/hopper/internal/github"
	"github.com/mcpeixoto/hopper/internal/store"
)

// GitHubRunner holds the optional GitHub Actions runner integration config.
type GitHubRunner struct {
	Client        *github.Client // nil disables the integration
	WebhookSecret string
	RunnerImage   string
	TriggerLabels []string // workflow runs-on labels that activate Hopper
	JobLabels     []string // Hopper node labels runner jobs are routed to
}

// GitHubWebhook receives GitHub workflow_job events and, for queued jobs tagged
// with the configured trigger labels, submits an ephemeral-runner Hopper job.
// Authentication is by HMAC signature (no bearer token).
func (a *API) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	gh := a.GitHub
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	if !github.VerifySignature(gh.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, http.StatusUnauthorized, "bad signature")
		return
	}
	if r.Header.Get("X-GitHub-Event") != "workflow_job" {
		writeJSON(w, http.StatusOK, map[string]string{"ignored": "not a workflow_job event"})
		return
	}
	ev, err := github.ParseWorkflowJob(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if ev.Action != "queued" {
		writeJSON(w, http.StatusOK, map[string]string{"ignored": "action=" + ev.Action})
		return
	}
	if !github.HasAllLabels(ev.WorkflowJob.Labels, gh.TriggerLabels) {
		writeJSON(w, http.StatusOK, map[string]string{"ignored": "labels do not match trigger"})
		return
	}
	if gh.Client == nil {
		writeError(w, http.StatusServiceUnavailable, "github runner integration not configured")
		return
	}

	token, err := gh.Client.RegistrationToken(r.Context(), ev.Repository.FullName)
	if err != nil {
		log.Printf("github webhook: registration token: %v", err)
		writeError(w, http.StatusBadGateway, "could not mint runner token")
		return
	}

	job, err := a.Store.SubmitJob(store.JobSpec{
		Image:  gh.RunnerImage,
		Labels: gh.JobLabels,
		Env: map[string]string{
			"REPO_URL":            ev.Repository.HTMLURL,
			"RUNNER_TOKEN":        token,
			"RUNNER_NAME":         fmt.Sprintf("hopper-%d", ev.WorkflowJob.ID),
			"LABELS":              strings.Join(ev.WorkflowJob.Labels, ","),
			"EPHEMERAL":           "true",
			"DISABLE_AUTO_UPDATE": "true",
		},
		MaxAttempts: 1, // CI runs must not auto-retry
		TimeoutS:    3 * 3600,
		SubmittedBy: "github:" + ev.Repository.FullName,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not enqueue runner job")
		return
	}
	log.Printf("github webhook: queued ephemeral runner job %s for %s", job.ID, ev.Repository.FullName)
	writeJSON(w, http.StatusCreated, map[string]string{"job_id": job.ID})
}
