// Package agent is the Hopper worker: it registers with the control plane,
// long-polls for jobs, runs each job's docker image, and reports the result. It
// is a pure HTTP client of the control plane — that is what lets a worker behind
// NAT participate with no inbound port. A node can run several jobs in parallel
// (HOPPER_CONCURRENCY).
package agent

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mcpeixoto/hopper/internal/archive"
	"github.com/mcpeixoto/hopper/internal/client"
	"github.com/mcpeixoto/hopper/internal/config"
	"github.com/mcpeixoto/hopper/internal/runner"
	"github.com/mcpeixoto/hopper/internal/version"
)

// heartbeatInterval is how often the agent renews its lease. It must be well
// under the control plane's lease duration (default 120s).
const heartbeatInterval = 30 * time.Second

// JobRunner runs a job's container. Implemented by *runner.Runner; an interface
// so tests can substitute a fake.
type JobRunner interface {
	Run(ctx context.Context, spec runner.Spec) (runner.Result, error)
}

// Status is a snapshot of the agent for the local node GUI.
type Status struct {
	WorkerID   string       `json:"worker_id"`
	Hostname   string       `json:"hostname"`
	Labels     []string     `json:"labels"`
	State      string       `json:"state"` // registering|idle|running|offline
	Version    string       `json:"version"`
	Slots      int          `json:"slots"`
	Running    []client.Job `json:"running"`
	JobsDone   int          `json:"jobs_done"`
	JobsFailed int          `json:"jobs_failed"`
	LastError  string       `json:"last_error,omitempty"`
	LastLog    string       `json:"last_log,omitempty"`
	StartedAt  string       `json:"started_at"`
	UpdatedAt  string       `json:"updated_at"`
}

// Agent runs the worker loop.
type Agent struct {
	Client   *client.Client
	Runner   JobRunner
	Cfg      config.AgentConfig
	WorkRoot string // base dir for per-job work directories
	Labels   []string

	mu         sync.Mutex
	workerID   string
	registered bool
	running    map[string]client.Job
	jobsDone   int
	jobsFailed int
	lastErr    string
	lastLog    string
	startedAt  string
}

// New builds an agent from config. It augments the configured labels with
// auto-detected os/arch labels (e.g. os:linux, arch:amd64) so jobs can target a
// platform across a mixed fleet.
func New(cfg config.AgentConfig, workRoot string) *Agent {
	labels := autoLabels(cfg.Labels)
	return &Agent{
		Client:    client.New(cfg.ControlURL, cfg.NodeToken),
		Runner:    runner.New(""),
		Cfg:       cfg,
		WorkRoot:  workRoot,
		Labels:    labels,
		running:   make(map[string]client.Job),
		startedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// autoLabels appends os:<goos> and arch:<goarch> to the user labels (deduped).
func autoLabels(user []string) []string {
	have := map[string]bool{}
	out := []string{}
	for _, l := range user {
		if !have[l] {
			have[l] = true
			out = append(out, l)
		}
	}
	for _, l := range []string{"os:" + runtime.GOOS, "arch:" + runtime.GOARCH} {
		if !have[l] {
			out = append(out, l)
		}
	}
	return out
}

// Status returns a snapshot of the agent for the local GUI.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	running := make([]client.Job, 0, len(a.running))
	for _, j := range a.running {
		running = append(running, j)
	}
	state := "registering"
	if a.registered {
		if len(running) > 0 {
			state = "running"
		} else {
			state = "idle"
		}
	}
	return Status{
		WorkerID: a.workerID, Hostname: a.Cfg.Hostname, Labels: a.Labels,
		State: state, Version: version.Version, Slots: a.slots(),
		Running: running, JobsDone: a.jobsDone, JobsFailed: a.jobsFailed,
		LastError: a.lastErr, LastLog: a.lastLog,
		StartedAt: a.startedAt, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func (a *Agent) slots() int {
	if a.Cfg.Concurrency > 1 {
		return a.Cfg.Concurrency
	}
	return 1
}

// Run registers, then runs `slots` concurrent claim loops until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	workerID, err := a.register(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.workerID = workerID
	a.registered = true
	a.mu.Unlock()

	go a.heartbeatLoop(ctx, workerID)
	n := a.slots()
	log.Printf("agent registered as %s (%s), slots=%d, labels=%v", workerID, a.Cfg.Hostname, n, a.Labels)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.claimLoop(ctx, workerID)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// claimLoop repeatedly claims and runs one job at a time (one slot).
func (a *Agent) claimLoop(ctx context.Context, workerID string) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := a.Client.ClaimJob(ctx, workerID, a.Labels)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.recordError("claim: " + err.Error())
			a.sleep(ctx, time.Duration(a.Cfg.PollInterval)*time.Second)
			continue
		}
		if job == nil {
			continue // long-poll timed out with no work
		}
		a.runJob(ctx, job)
	}
}

// register retries with backoff until the control plane answers or ctx ends.
func (a *Agent) register(ctx context.Context) (string, error) {
	backoff := time.Second
	for {
		w, err := a.Client.RegisterWorker(ctx, a.Cfg.Hostname, a.Labels)
		if err == nil {
			return w.ID, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		a.recordError("register: " + err.Error())
		log.Printf("agent register failed (%v), retrying in %s", err, backoff)
		a.sleep(ctx, backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) heartbeatLoop(ctx context.Context, workerID string) {
	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.Client.Heartbeat(ctx, workerID); err != nil && ctx.Err() == nil {
				log.Printf("agent heartbeat failed: %v", err)
			}
		}
	}
}

// runJob executes one job end to end and reports the result.
func (a *Agent) runJob(ctx context.Context, job *client.Job) {
	a.mu.Lock()
	a.running[job.ID] = *job
	a.mu.Unlock()
	log.Printf("running job %s image=%s", job.ID, job.Image)

	workDir := filepath.Join(a.WorkRoot, job.ID)
	inDir := filepath.Join(workDir, "in")
	outDir := filepath.Join(workDir, "out")
	_ = os.MkdirAll(inDir, 0o755)
	_ = os.MkdirAll(outDir, 0o755)
	defer os.RemoveAll(workDir)

	if job.InputArtifactID != "" {
		if err := a.downloadInput(ctx, job.ID, inDir); err != nil {
			a.finish(job, "failed", 0, "", "", "input download: "+err.Error())
			return
		}
	}

	spec := runner.Spec{
		JobID:      job.ID,
		Image:      job.Image,
		Command:    job.Command,
		Env:        job.Env,
		TimeoutS:   job.TimeoutS,
		InDir:      inDir,
		OutDir:     outDir,
		PullPolicy: a.Cfg.PullPolicy,
		AllowNet:   a.Cfg.AllowNet,
		CPULimit:   a.Cfg.CPULimit,
		MemLimit:   a.Cfg.MemLimit,
	}

	res, err := a.Runner.Run(ctx, spec)
	status, errMsg := "done", ""
	exit := res.ExitCode
	switch {
	case err != nil:
		status, errMsg = "failed", err.Error()
	case res.TimedOut:
		status, errMsg = "failed", "job timed out"
	case res.ExitCode != 0:
		status, errMsg = "failed", "non-zero exit"
	}

	// Report on a fresh context so a shutdown (which cancels ctx and kills the
	// container) still uploads results and completes the job — letting it requeue
	// immediately instead of waiting out the lease.
	reportCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logsID := a.uploadLogs(reportCtx, job.ID, res.Logs)
	outputID := ""
	if status == "done" && !archive.IsEmptyDir(outDir) {
		outputID = a.uploadOutput(reportCtx, job.ID, outDir)
	}
	a.complete(reportCtx, job, status, exit, outputID, logsID, errMsg, res.Logs)
}

// complete reports the outcome and records it; finish is the error shortcut.
func (a *Agent) complete(ctx context.Context, job *client.Job, status string, exit int, outputID, logsID, errMsg, logs string) {
	if cerr := a.Client.CompleteJob(ctx, job.ID, status, &exit, outputID, logsID, errMsg); cerr != nil {
		log.Printf("complete job %s failed: %v", job.ID, cerr)
		a.recordError("complete: " + cerr.Error())
	}
	a.mu.Lock()
	delete(a.running, job.ID)
	if status == "done" {
		a.jobsDone++
	} else {
		a.jobsFailed++
		a.lastErr = errMsg
	}
	if logs != "" {
		a.lastLog = tail(logs, 4000)
	}
	a.mu.Unlock()
	log.Printf("job %s finished status=%s exit=%d", job.ID, status, exit)
}

// finish reports a pre-run failure (e.g. input download) without a runner result.
func (a *Agent) finish(job *client.Job, status string, exit int, outputID, logsID, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.complete(ctx, job, status, exit, outputID, logsID, errMsg, "")
}

func (a *Agent) downloadInput(ctx context.Context, jobID, inDir string) error {
	rc, err := a.Client.DownloadInput(ctx, jobID)
	if err != nil {
		return err
	}
	defer rc.Close()
	return archive.UntarGz(rc, inDir)
}

func (a *Agent) uploadOutput(ctx context.Context, jobID, outDir string) string {
	tmp, err := os.CreateTemp("", "hopper-out-*.tgz")
	if err != nil {
		log.Printf("output tar: %v", err)
		return ""
	}
	defer os.Remove(tmp.Name())
	if err := archive.TarGz(outDir, tmp); err != nil {
		tmp.Close()
		log.Printf("output tar: %v", err)
		return ""
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return ""
	}
	defer tmp.Close()
	art, err := a.Client.UploadOutput(ctx, jobID, tmp)
	if err != nil {
		log.Printf("output upload: %v", err)
		return ""
	}
	return art.ID
}

func (a *Agent) uploadLogs(ctx context.Context, jobID, logs string) string {
	if logs == "" {
		return ""
	}
	art, err := a.Client.UploadLogs(ctx, jobID, strings.NewReader(logs))
	if err != nil {
		log.Printf("logs upload: %v", err)
		return ""
	}
	return art.ID
}

func (a *Agent) recordError(msg string) {
	a.mu.Lock()
	a.lastErr = msg
	a.mu.Unlock()
}

func (a *Agent) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
