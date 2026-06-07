package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mcpeixoto/hopper/internal/blob"
	"github.com/mcpeixoto/hopper/internal/livelog"
	"github.com/mcpeixoto/hopper/internal/store"
)

func newArtifactServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	blobs, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	live, err := livelog.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: db, Blob: blobs, LiveLog: live, LeaseSeconds: 60, LongPollSeconds: 1}
	srv := httptest.NewServer(Router(api, "", "", nil))
	t.Cleanup(srv.Close)
	return srv, db
}

func TestArtifactRoundTrip(t *testing.T) {
	srv, db := newArtifactServer(t)
	job, _ := db.SubmitJob(store.JobSpec{Image: "alpine"})

	// Operator uploads an input blob.
	resp := putBlob(t, srv.URL+"/api/jobs/"+job.ID+"/input", []byte("INPUT-DATA"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put input: want 201 got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The job now references an input artifact.
	got, _ := db.GetJob(job.ID)
	if got.InputArtifactID == "" {
		t.Fatal("input artifact not linked to job")
	}

	// Node downloads the input.
	dl := getURL(t, srv.URL+"/api/jobs/"+job.ID+"/input")
	body, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	if string(body) != "INPUT-DATA" {
		t.Fatalf("input download mismatch: %q", body)
	}

	// Node uploads output + logs.
	outResp := putBlob(t, srv.URL+"/api/jobs/"+job.ID+"/output", []byte("OUTPUT-DATA"))
	var outArt struct {
		ID string `json:"id"`
	}
	json.NewDecoder(outResp.Body).Decode(&outArt)
	outResp.Body.Close()
	logResp := putBlob(t, srv.URL+"/api/jobs/"+job.ID+"/logs", []byte("log line\n"))
	var logArt struct {
		ID string `json:"id"`
	}
	json.NewDecoder(logResp.Body).Decode(&logArt)
	logResp.Body.Close()
	if outArt.ID == "" || logArt.ID == "" {
		t.Fatal("missing artifact ids")
	}

	// Claim then complete with the artifact refs.
	do(t, "POST", srv.URL+"/api/jobs/claim", "", claimRequest{WorkerID: "w"}).Body.Close()
	exit := 0
	do(t, "POST", srv.URL+"/api/jobs/"+job.ID+"/complete", "",
		completeRequest{Status: "done", ExitCode: &exit, OutputArtifactID: outArt.ID, LogsRef: logArt.ID}).Body.Close()

	// Operator downloads the result + logs.
	res := getURL(t, srv.URL+"/api/jobs/"+job.ID+"/result")
	rb, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(rb) != "OUTPUT-DATA" {
		t.Fatalf("result mismatch: %q", rb)
	}
	lg := getURL(t, srv.URL+"/api/jobs/"+job.ID+"/logs")
	lb, _ := io.ReadAll(lg.Body)
	lg.Body.Close()
	if string(lb) != "log line\n" {
		t.Fatalf("logs mismatch: %q", lb)
	}
}

func getURL(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return resp
}

func TestLiveLogStreaming(t *testing.T) {
	srv, db := newArtifactServer(t)
	job, _ := db.SubmitJob(store.JobSpec{Image: "alpine"})

	// Worker appends live output while the job runs.
	putBlobPost(t, srv.URL+"/api/jobs/"+job.ID+"/logs/append", []byte("line 1\n")).Body.Close()
	putBlobPost(t, srv.URL+"/api/jobs/"+job.ID+"/logs/append", []byte("line 2\n")).Body.Close()

	// Operator reads the live log (no final artifact yet).
	res := getURL(t, srv.URL+"/api/jobs/"+job.ID+"/logs")
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "line 1\nline 2\n" {
		t.Fatalf("live log mismatch: %q", b)
	}

	// Once a final logs artifact exists, it takes precedence; live file is dropped.
	logResp := putBlob(t, srv.URL+"/api/jobs/"+job.ID+"/logs", []byte("FINAL LOGS"))
	var art struct {
		ID string `json:"id"`
	}
	json.NewDecoder(logResp.Body).Decode(&art)
	logResp.Body.Close()
	do(t, "POST", srv.URL+"/api/jobs/claim", "", claimRequest{WorkerID: "w"}).Body.Close()
	exit := 0
	do(t, "POST", srv.URL+"/api/jobs/"+job.ID+"/complete", "",
		completeRequest{Status: "done", ExitCode: &exit, LogsRef: art.ID}).Body.Close()

	res = getURL(t, srv.URL+"/api/jobs/"+job.ID+"/logs")
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "FINAL LOGS" {
		t.Fatalf("final log mismatch: %q", b)
	}
}

func putBlobPost(t *testing.T, url string, data []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

func TestResultBeforeDone(t *testing.T) {
	srv, db := newArtifactServer(t)
	job, _ := db.SubmitJob(store.JobSpec{Image: "alpine"})
	res := getURL(t, srv.URL+"/api/jobs/"+job.ID+"/result")
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202 for unfinished job, got %d", res.StatusCode)
	}
}

func putBlob(t *testing.T, url string, data []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	return resp
}
