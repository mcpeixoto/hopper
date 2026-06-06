// Package agent is the Hopper worker: it registers with the control plane,
// long-polls for jobs, runs each job's docker image, and reports the result. It
// is a pure HTTP client of the control plane — that is what lets a worker behind
// NAT participate with no inbound port.
package agent

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	WorkerID   string      `json:"worker_id"`
	Hostname   string      `json:"hostname"`
	Labels     []string    `json:"labels"`
	State      string      `json:"state"` // registering|idle|running|offline
	Version    string      `json:"version"`
	CurrentJob *client.Job `json:"current_job"`
	JobsDone   int         `json:"jobs_done"`
	JobsFailed int         `json:"jobs_failed"`
	LastError  string      `json:"last_error,omitempty"`
	LastLog    string      `json:"last_log,omitempty"`
	StartedAt  string      `json:"started_at"`
	UpdatedAt  string      `json:"updated_at"`
}

// Agent runs the worker loop.
type Agent struct {
	Client   *client.Client
	Runner   JobRunner
	Cfg      config.AgentConfig
	WorkRoot string // base dir for per-job work directories

	mu     sync.Mutex
	status Status
}

// New builds an agent from config.
func New(cfg config.AgentConfig, workRoot string) *Agent {
	a := &Agent{
		Client:   client.New(cfg.ControlURL, cfg.NodeToken),
		Runner:   runner.New(""),
		Cfg:      cfg,
		WorkRoot: workRoot,
	}
	a.status = Status{
		Hostname:  cfg.Hostname,
		Labels:    cfg.Labels,
		State:     "registering",
		Version:   version.Version,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return a
}

// Status returns a copy of the current status for the local GUI.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *Agent) setState(state string) {
	a.mu.Lock()
	a.status.State = state
	a.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	a.mu.Unlock()
}

// Run registers and then loops claiming and running jobs until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	workerID, err := a.register(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.status.WorkerID = workerID
	a.mu.Unlock()

	go a.heartbeatLoop(ctx, workerID)
	log.Printf("agent registered as %s (%s), labels=%v", workerID, a.Cfg.Hostname, a.Cfg.Labels)

	for {
		if ctx.Err() != nil {
			a.setState("offline")
			return ctx.Err()
		}
		a.setState("idle")
		job, err := a.Client.ClaimJob(ctx, workerID, a.Cfg.Labels)
		if err != nil {
			if ctx.Err() != nil {
				a.setState("offline")
				return ctx.Err()
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
		w, err := a.Client.RegisterWorker(ctx, a.Cfg.Hostname, a.Cfg.Labels)
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
	a.status.State = "running"
	a.status.CurrentJob = job
	a.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	a.mu.Unlock()
	log.Printf("running job %s image=%s", job.ID, job.Image)

	workDir := filepath.Join(a.WorkRoot, job.ID)
	inDir := filepath.Join(workDir, "in")
	outDir := filepath.Join(workDir, "out")
	_ = os.MkdirAll(inDir, 0o755)
	_ = os.MkdirAll(outDir, 0o755)
	defer os.RemoveAll(workDir)

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
	status := "done"
	errMsg := ""
	exit := res.ExitCode
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	} else if res.TimedOut {
		status = "failed"
		errMsg = "job timed out"
	} else if res.ExitCode != 0 {
		status = "failed"
		errMsg = "non-zero exit"
	}

	if cerr := a.Client.CompleteJob(ctx, job.ID, status, &exit, "", errMsg); cerr != nil {
		log.Printf("complete job %s failed: %v", job.ID, cerr)
		a.recordError("complete: " + cerr.Error())
	}

	a.mu.Lock()
	a.status.CurrentJob = nil
	a.status.LastLog = tail(res.Logs, 4000)
	if status == "done" {
		a.status.JobsDone++
	} else {
		a.status.JobsFailed++
		a.status.LastError = errMsg
	}
	a.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	a.mu.Unlock()
	log.Printf("job %s finished status=%s exit=%d", job.ID, status, exit)
}

func (a *Agent) recordError(msg string) {
	a.mu.Lock()
	a.status.LastError = msg
	a.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	a.mu.Unlock()
}

// sleep waits d or until ctx is cancelled.
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

// tail returns the last n characters of s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
