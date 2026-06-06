// Package runner executes a job's docker image on a worker node. It shells out to
// the docker CLI (via os/exec) rather than using a Docker SDK: the CLI is already
// required on any node, so depending on it adds nothing and keeps the binary lean.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"time"
)

// Spec describes a single container run.
type Spec struct {
	JobID      string
	Image      string
	Command    []string
	Env        map[string]string
	TimeoutS   int
	InDir      string // host path mounted read-only at /work/in
	OutDir     string // host path mounted read-write at /work/out
	PullPolicy string // "always" | "if-not-present"
	AllowNet   bool   // when false, the container runs with --network none
	CPULimit   string // docker --cpus value ("" = unset)
	MemLimit   string // docker --memory value ("" = unset)
}

// Result is the outcome of a run.
type Result struct {
	ExitCode int
	Logs     string
	TimedOut bool
}

// Runner runs jobs with a configured docker binary.
type Runner struct {
	Docker string // docker binary name/path; defaults to "docker"
}

// New returns a Runner using the given docker binary ("" -> "docker").
func New(docker string) *Runner {
	if docker == "" {
		docker = "docker"
	}
	return &Runner{Docker: docker}
}

// BuildRunArgs constructs the `docker run` argument vector for spec. It is pure
// (no side effects) so it can be unit-tested without docker installed.
func BuildRunArgs(spec Spec) []string {
	args := []string{"run", "--rm", "--name", "hopper-" + spec.JobID}

	if spec.InDir != "" {
		args = append(args, "-v", spec.InDir+":/work/in:ro")
	}
	if spec.OutDir != "" {
		args = append(args, "-v", spec.OutDir+":/work/out")
	}
	args = append(args, "-w", "/work")

	// Deterministic env ordering for stable output and tests.
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}

	if spec.CPULimit != "" {
		args = append(args, "--cpus", spec.CPULimit)
	}
	if spec.MemLimit != "" {
		args = append(args, "--memory", spec.MemLimit)
	}
	if !spec.AllowNet {
		args = append(args, "--network", "none")
	}

	args = append(args, spec.Image)
	args = append(args, spec.Command...)
	return args
}

// Pull fetches the image if the policy requires it. With "if-not-present" it is a
// no-op because `docker run` pulls a missing image automatically.
func (r *Runner) Pull(ctx context.Context, spec Spec) error {
	if spec.PullPolicy != "always" {
		return nil
	}
	cmd := exec.CommandContext(ctx, r.Docker, "pull", spec.Image)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker pull %s: %v: %s", spec.Image, err, out)
	}
	return nil
}

// Run pulls (per policy) then runs the container, capturing combined output and
// the exit code, enforcing the job timeout. A non-zero exit code is returned in
// Result (not as an error); err is non-nil only for runner-level failures such as
// docker being missing or the image failing to pull.
func (r *Runner) Run(ctx context.Context, spec Spec) (Result, error) {
	if err := r.Pull(ctx, spec); err != nil {
		return Result{}, err
	}

	timeout := time.Duration(spec.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var buf bytes.Buffer
	cmd := exec.CommandContext(runCtx, r.Docker, BuildRunArgs(spec)...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	res := Result{Logs: buf.String()}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = 124 // conventional timeout exit code
		// Best-effort: ensure the container is gone.
		_ = exec.Command(r.Docker, "rm", "-f", "hopper-"+spec.JobID).Run()
		return res, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		// Could not start docker at all.
		return res, fmt.Errorf("docker run: %w", err)
	}
	res.ExitCode = 0
	return res, nil
}
