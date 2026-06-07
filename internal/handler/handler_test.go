package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mcpeixoto/hopper/internal/store"
)

func newTestServer(t *testing.T, operatorToken, nodeToken string) *httptest.Server {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	api := &API{Store: db, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(Router(api, operatorToken, nodeToken, []string{"http://localhost:5173"}))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	return resp
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t, "", "")
	resp := do(t, "GET", srv.URL+"/health", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv := newTestServer(t, "", "")
	resp := do(t, "GET", srv.URL+"/api/version", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	if _, ok := out["version"]; !ok {
		t.Fatalf("expected a version field, got %v", out)
	}
}

func TestSubmitRequiresOperatorToken(t *testing.T) {
	srv := newTestServer(t, "op-secret", "node-secret")

	// No token -> 401.
	resp := do(t, "POST", srv.URL+"/api/jobs", "", store.JobSpec{Image: "alpine"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", resp.StatusCode)
	}

	// Wrong token -> 401.
	resp = do(t, "POST", srv.URL+"/api/jobs", "wrong", store.JobSpec{Image: "alpine"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 with wrong token, got %d", resp.StatusCode)
	}

	// Correct token -> 201.
	resp = do(t, "POST", srv.URL+"/api/jobs", "op-secret", store.JobSpec{Image: "alpine"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201 with token, got %d", resp.StatusCode)
	}
}

func TestNodeTokenSeparation(t *testing.T) {
	srv := newTestServer(t, "op-secret", "node-secret")
	// Operator token must NOT open the node plane.
	resp := do(t, "POST", srv.URL+"/api/jobs/claim", "op-secret", claimRequest{WorkerID: "w"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("operator token should not access node plane, got %d", resp.StatusCode)
	}
}

func TestSubmitClaimComplete(t *testing.T) {
	srv := newTestServer(t, "", "") // auth disabled for the flow test

	// Submit.
	resp := do(t, "POST", srv.URL+"/api/jobs", "", store.JobSpec{Image: "alpine", Command: []string{"echo", "hi"}})
	var job store.Job
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	if job.ID == "" {
		t.Fatal("no job id returned")
	}

	// Register worker.
	resp = do(t, "POST", srv.URL+"/api/workers/register", "", registerRequest{Hostname: "laptop"})
	var w store.Worker
	json.NewDecoder(resp.Body).Decode(&w)
	resp.Body.Close()

	// Claim.
	resp = do(t, "POST", srv.URL+"/api/jobs/claim", "", claimRequest{WorkerID: w.ID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 from claim, got %d", resp.StatusCode)
	}
	var claimed store.Job
	json.NewDecoder(resp.Body).Decode(&claimed)
	resp.Body.Close()
	if claimed.ID != job.ID {
		t.Fatalf("claimed wrong job: %s != %s", claimed.ID, job.ID)
	}

	// Complete.
	exit := 0
	resp = do(t, "POST", srv.URL+"/api/jobs/"+job.ID+"/complete", "",
		completeRequest{Status: "done", ExitCode: &exit})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 from complete, got %d", resp.StatusCode)
	}

	// Verify done.
	resp = do(t, "GET", srv.URL+"/api/jobs/"+job.ID, "", nil)
	var final store.Job
	json.NewDecoder(resp.Body).Decode(&final)
	resp.Body.Close()
	if final.Status != "done" {
		t.Fatalf("want done, got %q", final.Status)
	}
}

func TestClaimLongPollReturns204WhenEmpty(t *testing.T) {
	srv := newTestServer(t, "", "") // LongPollSeconds=1
	resp := do(t, "POST", srv.URL+"/api/jobs/claim", "", claimRequest{WorkerID: "w"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("want 204 on empty queue, got %d", resp.StatusCode)
	}
}
