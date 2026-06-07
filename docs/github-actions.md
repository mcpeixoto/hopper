# Distributed GitHub Actions runners

Hopper can run your GitHub Actions jobs on your own fleet — **ephemeral self-hosted runners
without Kubernetes**. When a workflow job is queued, GitHub pings Hopper, Hopper mints a
just-in-time runner token and enqueues a normal Hopper job that runs the official runner
image in `--ephemeral` mode. A free node claims it, the runner executes exactly one workflow
run, then the container is discarded.

```
GitHub workflow_job:queued ──webhook──▶ hopperd ──mint JIT token──▶ submit runner job
                                                                          │
                                       free CI node claims it ◀───────────┘
                                       docker run github-runner --ephemeral → runs 1 job → exits
```

## Setup

### 1. A GitHub token

Create a PAT (classic, `repo` scope) or a GitHub App installation token that can call the
repo's `actions/runners/registration-token` endpoint. Give it to `hopperd`:

```bash
HOPPER_GITHUB_TOKEN=ghp_xxx
HOPPER_GITHUB_WEBHOOK_SECRET=$(openssl rand -hex 32)
```

### 2. The webhook

In the repo (or org): **Settings → Webhooks → Add webhook**
- Payload URL: `https://hopper.example.com/api/github/webhook`
- Content type: `application/json`
- Secret: the `HOPPER_GITHUB_WEBHOOK_SECRET` value
- Events: **Workflow jobs**

Hopper verifies every delivery's HMAC signature against the secret.

### 3. CI-capable nodes

Runner jobs need network access and should only land on nodes you've designated for CI. Run
those agents with:

```bash
HOPPER_LABELS=ci          # matches HOPPER_RUNNER_JOB_LABELS (default: ci)
HOPPER_ALLOW_NET=1        # the runner must reach github.com
```

### 4. Use it in a workflow

```yaml
jobs:
  build:
    runs-on: [self-hosted, hopper]   # matches HOPPER_RUNNER_TRIGGER_LABELS
    steps:
      - uses: actions/checkout@v4
      - run: make test
```

## Tuning

| Variable | Default | Meaning |
|----------|---------|---------|
| `HOPPER_GITHUB_TOKEN` | _(off)_ | enables the integration; mints runner tokens |
| `HOPPER_GITHUB_WEBHOOK_SECRET` | _(none)_ | HMAC secret; **set this in production** |
| `HOPPER_RUNNER_IMAGE` | `myoung34/github-runner:latest` | ephemeral runner image |
| `HOPPER_RUNNER_TRIGGER_LABELS` | `self-hosted,hopper` | workflow `runs-on` labels that activate Hopper |
| `HOPPER_RUNNER_JOB_LABELS` | `ci` | Hopper node labels runner jobs route to |

## Limitations & security

- **Trust:** a runner executes arbitrary repo code as the container user, with network on.
  Only enable this for **your own repositories** (or trusted forks). The
  [trust model](security.md) applies in full.
- **Container-based steps / `services:`** need Docker inside the runner. Enable it per CI node
  by mounting the host Docker socket: set `HOPPER_MOUNT_DOCKER_SOCKET=1` on the agent. Jobs on
  that node then get `/var/run/docker.sock` and can run container steps. This grants the
  container full control of the host Docker daemon (root-equivalent) — only enable on nodes you
  dedicate to trusted CI.
- Runner jobs have `max_attempts=1` — a failed CI run is never silently retried.
