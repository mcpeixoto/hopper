// Command hopper is the operator CLI for the control plane: submit jobs, watch
// the queue and fleet, and fetch results. It reads HOPPER_CONTROL_URL and
// HOPPER_OPERATOR_TOKEN from the environment.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mcpeixoto/hopper/internal/archive"
	"github.com/mcpeixoto/hopper/internal/client"
	"github.com/mcpeixoto/hopper/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	c := client.New(env("HOPPER_CONTROL_URL", "http://localhost:8080"), os.Getenv("HOPPER_OPERATOR_TOKEN"))
	ctx := context.Background()
	var err error

	switch os.Args[1] {
	case "submit":
		err = cmdSubmit(ctx, c, os.Args[2:])
	case "jobs":
		err = cmdJobs(ctx, c, os.Args[2:])
	case "get":
		err = cmdGet(ctx, c, os.Args[2:])
	case "logs":
		err = cmdLogs(ctx, c, os.Args[2:])
	case "result":
		err = cmdResult(ctx, c, os.Args[2:])
	case "cancel":
		err = cmdSimple(ctx, os.Args[2:], c.CancelJob, "cancelled")
	case "nodes":
		err = cmdNodes(ctx, c)
	case "schedule":
		err = cmdSchedule(ctx, c, os.Args[2:])
	case "version":
		fmt.Println(version.Version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hopper: "+err.Error())
		os.Exit(1)
	}
}

func cmdSubmit(ctx context.Context, c *client.Client, args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	image := fs.String("image", "", "container image (required)")
	cmd := fs.String("cmd", "", "command to run (space-separated)")
	labels := fs.String("labels", "", "required node labels (comma-separated)")
	prio := fs.Int("priority", 0, "priority (higher runs first)")
	timeout := fs.Int("timeout", 0, "timeout seconds")
	inputPath := fs.String("input", "", "directory to send as the job's /work/in")
	wait := fs.Bool("wait", false, "wait for the job to finish")
	_ = fs.Parse(args)
	if *image == "" {
		return fmt.Errorf("--image is required")
	}

	spec := client.JobSpec{
		Image:    *image,
		Command:  fields(*cmd),
		Labels:   csv(*labels),
		Priority: *prio,
		TimeoutS: *timeout,
		Paused:   *inputPath != "",
	}
	job, err := c.SubmitJob(ctx, spec)
	if err != nil {
		return err
	}

	if *inputPath != "" {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(archive.TarGz(*inputPath, pw)) }()
		if _, err := c.UploadInput(ctx, job.ID, pr); err != nil {
			return fmt.Errorf("upload input: %w", err)
		}
		if err := c.ReleaseJob(ctx, job.ID); err != nil {
			return fmt.Errorf("release: %w", err)
		}
	}
	fmt.Println(job.ID)

	if *wait {
		return waitForJob(ctx, c, job.ID)
	}
	return nil
}

func waitForJob(ctx context.Context, c *client.Client, id string) error {
	for {
		j, err := c.GetJob(ctx, id)
		if err != nil {
			return err
		}
		switch j.Status {
		case "done":
			fmt.Println("done (exit 0)")
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("job %s: %s", j.Status, j.Error)
		}
		time.Sleep(time.Second)
	}
}

func cmdJobs(ctx context.Context, c *client.Client, args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ExitOnError)
	status := fs.String("status", "", "filter by status")
	_ = fs.Parse(args)
	jobs, err := c.ListJobs(ctx, *status)
	if err != nil {
		return err
	}
	fmt.Printf("%-20s %-10s %-26s %-8s\n", "ID", "STATUS", "IMAGE", "TRY")
	for _, j := range jobs {
		fmt.Printf("%-20s %-10s %-26s %d/%d\n", j.ID, j.Status, trunc(j.Image, 26), j.Attempts, j.MaxAttempts)
	}
	return nil
}

func cmdGet(ctx context.Context, c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: hopper get <job-id>")
	}
	j, err := c.GetJob(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("id:       %s\nimage:    %s\nstatus:   %s\nnode:     %s\nattempts: %d/%d\nexit:     %v\nerror:    %s\ncreated:  %s\nupdated:  %s\n",
		j.ID, j.Image, j.Status, j.ClaimedBy, j.Attempts, j.MaxAttempts, exit(j.ExitCode), j.Error, j.CreatedAt, j.UpdatedAt)
	return nil
}

func cmdLogs(ctx context.Context, c *client.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: hopper logs <job-id>")
	}
	rc, err := c.DownloadLogs(ctx, args[0])
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(os.Stdout, rc)
	return err
}

func cmdResult(ctx context.Context, c *client.Client, args []string) error {
	fs := flag.NewFlagSet("result", flag.ExitOnError)
	out := fs.String("o", "", "output directory to extract into (default: stdout tar.gz)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: hopper result [-o DIR] <job-id>")
	}
	rc, err := c.DownloadResult(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	defer rc.Close()
	if *out == "" {
		_, err = io.Copy(os.Stdout, rc)
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := archive.UntarGz(rc, *out); err != nil {
		return err
	}
	fmt.Printf("extracted result to %s\n", *out)
	return nil
}

func cmdSchedule(ctx context.Context, c *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: hopper schedule list | create | rm <id>")
	}
	switch args[0] {
	case "list":
		scs, err := c.ListSchedules(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%-20s %-14s %-22s %s\n", "ID", "CRON", "IMAGE", "NEXT RUN")
		for _, s := range scs {
			fmt.Printf("%-20s %-14s %-22s %s\n", s.ID, s.Cron, trunc(s.Spec.Image, 22), s.NextRun)
		}
		return nil
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: hopper schedule rm <id>")
		}
		if err := c.DeleteSchedule(ctx, args[1]); err != nil {
			return err
		}
		fmt.Println("deleted")
		return nil
	case "create":
		fs := flag.NewFlagSet("schedule create", flag.ExitOnError)
		name := fs.String("name", "", "schedule name")
		cronExpr := fs.String("cron", "", "cron expression, e.g. '0 2 * * *' (required)")
		image := fs.String("image", "", "container image (required)")
		cmd := fs.String("cmd", "", "command (space-separated)")
		labels := fs.String("labels", "", "required node labels (comma-separated)")
		_ = fs.Parse(args[1:])
		if *cronExpr == "" || *image == "" {
			return fmt.Errorf("--cron and --image are required")
		}
		sc, err := c.CreateSchedule(ctx, *name, *cronExpr, client.JobSpec{
			Image: *image, Command: fields(*cmd), Labels: csv(*labels),
		})
		if err != nil {
			return err
		}
		fmt.Printf("%s (next run %s)\n", sc.ID, sc.NextRun)
		return nil
	default:
		return fmt.Errorf("unknown schedule subcommand %q", args[0])
	}
}

func cmdNodes(ctx context.Context, c *client.Client) error {
	workers, err := c.ListWorkers(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%-20s %-12s %-8s %s\n", "HOSTNAME", "STATUS", "LABELS", "LAST HEARTBEAT")
	for _, w := range workers {
		fmt.Printf("%-20s %-12s %-8s %s\n", trunc(w.Hostname, 20), w.Status, strings.Join(w.Labels, ","), w.LastHeartbeat)
	}
	return nil
}

func cmdSimple(ctx context.Context, args []string, fn func(context.Context, string) error, ok string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected a job id")
	}
	if err := fn(ctx, args[0]); err != nil {
		return err
	}
	fmt.Println(ok)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `hopper — operator CLI

  hopper submit --image IMG [--cmd "..."] [--labels a,b] [--input DIR] [--wait]
  hopper jobs [--status queued|in_flight|done|failed]
  hopper get <id>
  hopper logs <id>
  hopper result [-o DIR] <id>
  hopper cancel <id>
  hopper nodes
  hopper schedule list | create --cron "0 2 * * *" --image IMG [--cmd ...] | rm <id>
  hopper version

Env: HOPPER_CONTROL_URL, HOPPER_OPERATOR_TOKEN`)
	os.Exit(2)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func fields(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Fields(s)
}
func csv(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
func exit(p *int) any {
	if p == nil {
		return "—"
	}
	return *p
}
