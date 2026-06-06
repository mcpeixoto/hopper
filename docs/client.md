# Worker nodes (`hopper-agent`)

A worker is any machine with Docker that runs `hopper-agent`. It registers with the control
plane, long-polls for jobs, runs them, reports results, and serves a local **node console**.
It only ever makes **outbound** connections, so it works behind NAT with no inbound port.

## One-command onboarding

On the node:

```bash
curl -fsSL https://raw.githubusercontent.com/mcpeixoto/hopper/main/scripts/install-worker.sh | \
  HOPPER_CONTROL_URL=https://hopper.example.com \
  HOPPER_NODE_TOKEN=xxxxxxxx \
  HOPPER_LABELS=cpu \
  bash
```

The script:
1. checks Docker is installed and the daemon is reachable,
2. downloads the matching `hopper-agent` release binary into `/usr/local/bin`,
3. writes `/etc/hopper/agent.env` (mode 600) with your settings,
4. installs and starts a `hopper-agent` **systemd** service (`Restart=always`), or prints a
   `launchd`/loop fallback on systems without systemd (e.g. macOS).

## Run manually

```bash
export HOPPER_CONTROL_URL=https://hopper.example.com
export HOPPER_NODE_TOKEN=xxxxxxxx
export HOPPER_LABELS=cpu,bigmem
./bin/hopper-agent
```

## The node console

The agent serves a local status API (and, once built in, the node GUI) on
`HOPPER_AGENT_ADDR` (default `127.0.0.1:8765`) — loopback only, so it's private to that
machine:

```bash
curl -s localhost:8765/api/status | jq
# { "state": "running", "current_job": {...}, "jobs_done": 12, "version": "v0.2.0", ... }
```

## Labels & capabilities

`HOPPER_LABELS` is a comma-separated list of what this node can do (`gpu`, `bigmem`,
`fast-disk`, whatever you define). A job is only offered to a worker that has *all* the
labels the job requires. No labels = a general-purpose node that takes any unlabelled job.

Every agent also **auto-advertises** `os:<goos>` and `arch:<goarch>` (e.g. `os:linux`,
`arch:arm64`), so in a mixed fleet you can target a platform with
`--labels os:linux,arch:amd64` without configuring anything.

## Running jobs in parallel

By default a node runs one job at a time. Set `HOPPER_CONCURRENCY=N` to run up to N jobs
concurrently — turn a big server into an N-slot runner. The node console shows
`Running x/N`.

## Private images

To pull from a private registry, either `docker login` on the node yourself, or let the
agent do it: set `HOPPER_REGISTRY_AUTH=1` plus `HOPPER_REGISTRY_SERVER` (e.g. `ghcr.io`),
`HOPPER_REGISTRY_USER`, and `HOPPER_REGISTRY_PASSWORD`. The agent logs in once at startup.

## Resource limits & isolation

- `HOPPER_CPU` / `HOPPER_MEM` cap each job container (`--cpus` / `--memory`).
- By default containers run with `--network none`. Set `HOPPER_ALLOW_NET=1` only for jobs
  that genuinely need network.
- Inputs are mounted read-only at `/work/in`; outputs go in `/work/out`.

> Containers run as the Docker daemon (root-ish). Only run a worker against a control plane
> you trust completely — see [security.md](security.md).

## Graceful shutdown

`SIGINT`/`SIGTERM` stops the agent; systemd sends `SIGTERM` on stop/upgrade. A job in flight
that gets interrupted is requeued by the control plane's reaper once its lease expires.

## Updating

Set `HOPPER_AUTOUPDATE=1` and the agent self-updates to the latest release on its check
interval, re-execing into the new binary (systemd relaunches it). See [releases.md](releases.md).
