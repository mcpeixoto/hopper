# `hopper` — operator CLI

A small command-line client for the control plane. It reads:

- `HOPPER_CONTROL_URL` (default `http://localhost:8080`)
- `HOPPER_OPERATOR_TOKEN`

Build it with `make build` (→ `bin/hopper`) or download it from
[Releases](https://github.com/mcpeixoto/hopper/releases).

## Commands

```bash
# Submit a job and wait for it to finish
hopper submit --image alpine:3.20 --cmd "echo hello" --wait

# Submit with inputs: tar a directory, send it as /work/in, run, then fetch output
hopper submit --image mytool:latest --cmd "process /work/in -o /work/out" --input ./data
hopper result -o ./out <job-id>          # extracts the output tar.gz into ./out

# Inspect
hopper jobs                              # list (optionally --status queued|in_flight|done|failed)
hopper get <job-id>                      # full detail
hopper logs <job-id>                     # captured stdout/stderr

# Fleet & nodes
hopper nodes                             # fleet with load / slots / image-cache count
hopper node <worker-id>                  # node detail: cpu, load, mem, disk, cached images
hopper images [--image alpine]           # which nodes have which docker images cached

# Control
hopper cancel <job-id>
hopper version
```

## Flags for `submit`

| Flag | Meaning |
|------|---------|
| `--image` | container image (required) |
| `--cmd` | command to run (space-separated) |
| `--labels` | required node labels (comma-separated) |
| `--priority` | higher runs first |
| `--timeout` | timeout in seconds |
| `--input DIR` | tar `DIR` and send it as the job's `/work/in` (submits paused, attaches, releases) |
| `--wait` | block until the job finishes (non-zero exit on failure) |

## Example: a quick build farm

```bash
export HOPPER_CONTROL_URL=https://hopper.example.com
export HOPPER_OPERATOR_TOKEN=...

hopper submit --image golang:1.22 --input ./myrepo \
  --cmd "sh -c 'cd /work/in && go build -o /work/out/app ./...'" --wait
hopper result -o ./artifacts <job-id>
ls ./artifacts/app
```
