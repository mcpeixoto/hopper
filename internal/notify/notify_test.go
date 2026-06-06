package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNilNotifierNoop(t *testing.T) {
	var n *Notifier
	n.Fire(Event{JobID: "x"}) // must not panic
	if New("", "secret") != nil {
		t.Fatal("empty url should yield nil notifier")
	}
}

func TestSendSignsAndDelivers(t *testing.T) {
	got := make(chan []byte, 1)
	sig := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- b
		sig <- r.Header.Get("X-Hopper-Signature-256")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := New(srv.URL, "s3cr3t")
	n.Fire(Event{Event: "job.done", JobID: "job_1", Status: "done"})

	select {
	case body := <-got:
		var ev Event
		if err := json.Unmarshal(body, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.JobID != "job_1" || ev.Status != "done" {
			t.Fatalf("bad event: %#v", ev)
		}
		mac := hmac.New(sha256.New, []byte("s3cr3t"))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if s := <-sig; s != want {
			t.Fatalf("signature mismatch: %s != %s", s, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not delivered")
	}
}
