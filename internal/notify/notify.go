// Package notify posts a JSON event to a configured webhook when a job reaches a
// terminal state, so external systems (Slack via a relay, a dashboard, an
// automation) can react. Deliveries are best-effort and fired asynchronously.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Event is the payload delivered on a terminal job transition.
type Event struct {
	Event       string `json:"event"` // "job.done" | "job.failed" | "job.cancelled"
	JobID       string `json:"job_id"`
	Status      string `json:"status"`
	Image       string `json:"image"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	Error       string `json:"error,omitempty"`
	Attempts    int    `json:"attempts"`
	SubmittedBy string `json:"submitted_by,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
}

// Notifier delivers events to a webhook URL, optionally HMAC-signing the body.
type Notifier struct {
	URL    string
	Secret string
	HTTP   *http.Client
}

// New returns a Notifier, or nil if url is empty (notifications disabled).
func New(url, secret string) *Notifier {
	if url == "" {
		return nil
	}
	return &Notifier{URL: url, Secret: secret, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Fire delivers the event in the background (best-effort). Safe to call on a nil
// Notifier (no-op).
func (n *Notifier) Fire(ev Event) {
	if n == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := n.send(ctx, ev); err != nil {
			log.Printf("notify: %v", err)
		}
	}()
}

func (n *Notifier) send(ctx context.Context, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hopper-notifier")
	if n.Secret != "" {
		mac := hmac.New(sha256.New, []byte(n.Secret))
		mac.Write(body)
		req.Header.Set("X-Hopper-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := n.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &httpError{resp.StatusCode}
	}
	return nil
}

type httpError struct{ code int }

func (e *httpError) Error() string { return "webhook returned " + http.StatusText(e.code) }
