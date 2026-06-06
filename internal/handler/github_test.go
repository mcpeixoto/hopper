package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mcpeixoto/hopper/internal/github"
	"github.com/mcpeixoto/hopper/internal/store"
)

func TestGitHubWebhookQueuesRunner(t *testing.T) {
	// Mock GitHub API that mints a registration token.
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"REG"}`))
	}))
	defer ghAPI.Close()

	db, _ := store.Open(":memory:")
	defer db.Close()
	api := &API{
		Store: db,
		GitHub: &GitHubRunner{
			Client:        &github.Client{Token: "tok", API: ghAPI.URL, HTTP: ghAPI.Client()},
			WebhookSecret: "whsec",
			RunnerImage:   "myoung34/github-runner:latest",
			TriggerLabels: []string{"self-hosted", "hopper"},
			JobLabels:     []string{"ci"},
		},
		LeaseSeconds:    60,
		LongPollSeconds: 1,
	}
	srv := httptest.NewServer(Router(api, "op", "node", nil))
	defer srv.Close()

	body := `{"action":"queued","workflow_job":{"id":7,"name":"build","labels":["self-hosted","hopper"]},"repository":{"full_name":"me/repo","html_url":"https://github.com/me/repo"}}`
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest("POST", srv.URL+"/api/github/webhook", bytes.NewBufferString(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}

	// A runner job should now be queued with the runner image + ci label.
	jobs, _ := db.ListJobs("queued")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 queued runner job, got %d", len(jobs))
	}
	j := jobs[0]
	if j.Image != "myoung34/github-runner:latest" || j.Env["RUNNER_TOKEN"] != "REG" {
		t.Fatalf("bad runner job: %#v", j)
	}
	if len(j.Labels) != 1 || j.Labels[0] != "ci" {
		t.Fatalf("runner job should target ci nodes: %#v", j.Labels)
	}
}

func TestGitHubWebhookRejectsBadSignature(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	api := &API{
		Store:  db,
		GitHub: &GitHubRunner{WebhookSecret: "whsec", TriggerLabels: []string{"hopper"}},
	}
	srv := httptest.NewServer(Router(api, "", "", nil))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/github/webhook", bytes.NewBufferString(`{}`))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for bad signature, got %d", resp.StatusCode)
	}
}
