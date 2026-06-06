package runner

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildRunArgsBasics(t *testing.T) {
	args := BuildRunArgs(Spec{
		JobID:   "job_abc",
		Image:   "alpine:3.20",
		Command: []string{"echo", "hello"},
		InDir:   "/work/in",
		OutDir:  "/work/out",
	})

	want := []string{
		"run", "--rm", "--name", "hopper-job_abc",
		"-v", "/work/in:/work/in:ro",
		"-v", "/work/out:/work/out",
		"-w", "/work",
		"--network", "none",
		"alpine:3.20", "echo", "hello",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("argv mismatch:\n got=%v\nwant=%v", args, want)
	}
}

func TestBuildRunArgsEnvSortedAndLimits(t *testing.T) {
	args := BuildRunArgs(Spec{
		JobID:    "j",
		Image:    "img",
		Env:      map[string]string{"B": "2", "A": "1"},
		CPULimit: "2",
		MemLimit: "512m",
		AllowNet: true,
	})
	joined := strings.Join(args, " ")

	// Env must be deterministic (sorted): A before B.
	ai := slices.Index(args, "A=1")
	bi := slices.Index(args, "B=2")
	if ai == -1 || bi == -1 || ai > bi {
		t.Fatalf("env not sorted/present: %v", args)
	}
	if !strings.Contains(joined, "--cpus 2") || !strings.Contains(joined, "--memory 512m") {
		t.Fatalf("limits missing: %s", joined)
	}
	// AllowNet=true means no --network none.
	if strings.Contains(joined, "--network none") {
		t.Fatalf("AllowNet should drop --network none: %s", joined)
	}
}

func TestBuildRunArgsNoMountsWhenUnset(t *testing.T) {
	args := BuildRunArgs(Spec{JobID: "j", Image: "img"})
	if slices.Contains(args, "-v") {
		t.Fatalf("did not expect volume mounts: %v", args)
	}
}
