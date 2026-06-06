package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := `{"hello":"world"}`
	good := sign("s3cr3t", body)
	if !VerifySignature("s3cr3t", []byte(body), good) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature("s3cr3t", []byte(body), sign("wrong", body)) {
		t.Fatal("invalid signature accepted")
	}
	if VerifySignature("s3cr3t", []byte(body), "garbage") {
		t.Fatal("malformed header accepted")
	}
	if !VerifySignature("", []byte(body), "") {
		t.Fatal("empty secret should disable verification")
	}
}

func TestParseWorkflowJob(t *testing.T) {
	body := []byte(`{
		"action":"queued",
		"workflow_job":{"id":42,"name":"build","labels":["self-hosted","hopper"]},
		"repository":{"full_name":"me/repo","html_url":"https://github.com/me/repo"}
	}`)
	ev, err := ParseWorkflowJob(body)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Action != "queued" || ev.WorkflowJob.ID != 42 || ev.Repository.FullName != "me/repo" {
		t.Fatalf("bad parse: %#v", ev)
	}
	if !HasAllLabels(ev.WorkflowJob.Labels, []string{"self-hosted", "hopper"}) {
		t.Fatal("labels should match")
	}
	if HasAllLabels(ev.WorkflowJob.Labels, []string{"gpu"}) {
		t.Fatal("missing label should not match")
	}
}

func TestRegistrationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/me/repo/actions/runners/registration-token" {
			http.Error(w, "wrong path", 400)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no auth", 401)
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"REG-TOKEN","expires_at":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	c := &Client{Token: "tok", API: srv.URL, HTTP: srv.Client()}
	tok, err := c.RegistrationToken(context.Background(), "me/repo")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "REG-TOKEN" {
		t.Fatalf("got %q", tok)
	}
}
