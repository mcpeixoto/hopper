# Configuration reference

All configuration is environment variables (see [`.env.example`](../.env.example)). There is
no config file.

## Control plane (`hopperd`)

| Variable | Default | Meaning |
|----------|---------|---------|
| `HOPPER_PORT` | `8080` | HTTP listen port |
| `HOPPER_DB_PATH` | `data/hopper.db` | SQLite file path, or a `postgres://…` DSN for Postgres |
| `HOPPER_ARTIFACT_DIR` | `data/artifacts` | directory for input/output/log blobs |
| `HOPPER_OPERATOR_TOKEN` | _(empty)_ | bearer token for submit/admin routes; empty = auth disabled |
| `HOPPER_NODE_TOKEN` | _(empty)_ | bearer token for the worker plane; empty = auth disabled |
| `HOPPER_CORS_ORIGINS` | `http://localhost:5173` | comma-separated browser origins for the console |
| `HOPPER_LEASE_SECONDS` | `120` | visibility timeout granted on claim |
| `HOPPER_LONGPOLL_SECONDS` | `25` | how long `/api/jobs/claim` blocks |
| `HOPPER_SUBMIT_RPM` | `0` | per-IP rate limit on submit + webhook (req/min; 0 = unlimited) |
| `HOPPER_JOB_RETENTION_DAYS` | `0` | purge terminal jobs + orphaned blobs older than N days (0 = keep forever) |
| `HOPPER_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `HOPPER_LOG_FORMAT` | `text` | `text` or `json` (structured) |
| `HOPPER_NOTIFY_URL` | _(off)_ | webhook POSTed a JSON event on terminal job transitions |
| `HOPPER_NOTIFY_SECRET` | _(none)_ | HMAC secret for the webhook (`X-Hopper-Signature-256`) |
| `HOPPER_AUTOUPDATE` | _(off)_ | set to `1` to self-update from releases |
| `HOPPER_UPDATE_INTERVAL_MIN` | `60` | minutes between update checks |

## Worker agent (`hopper-agent`)

| Variable | Default | Meaning |
|----------|---------|---------|
| `HOPPER_CONTROL_URL` | `http://localhost:8080` | control plane base URL (outbound only) |
| `HOPPER_NODE_TOKEN` | _(empty)_ | bearer token presented on the worker plane |
| `HOPPER_HOSTNAME` | OS hostname | identity reported on register |
| `HOPPER_LABELS` | _(none)_ | comma-separated capabilities, e.g. `gpu,bigmem` (auto-adds `os:<goos>`, `arch:<goarch>`) |
| `HOPPER_CONCURRENCY` | `1` | max jobs to run in parallel on this node |
| `HOPPER_PULL_POLICY` | `if-not-present` | `always` or `if-not-present` |
| `HOPPER_REGISTRY_AUTH` | _(off)_ | set to `1` to `docker login` from the vars below before pulling |
| `HOPPER_REGISTRY_SERVER` | _(Docker Hub)_ | registry host, e.g. `ghcr.io` |
| `HOPPER_REGISTRY_USER` | _(none)_ | registry username |
| `HOPPER_REGISTRY_PASSWORD` | _(none)_ | registry password / token (sent on stdin, not argv) |
| `HOPPER_ALLOW_NET` | _(off)_ | set to `1` to give job containers network (default `--network none`) |
| `HOPPER_MOUNT_DOCKER_SOCKET` | _(off)_ | mount the host docker socket into jobs (container-based/CI steps; root-equivalent) |
| `HOPPER_CPU` | _(unset)_ | docker `--cpus` cap per job, e.g. `2` |
| `HOPPER_MEM` | _(unset)_ | docker `--memory` cap per job, e.g. `512m` |
| `HOPPER_POLL_INTERVAL` | `2` | seconds between claim retries after an error |
| `HOPPER_AGENT_ADDR` | `127.0.0.1:8765` | bind address for the local node console / status API |
| `HOPPER_WORK_ROOT` | `data/work` | per-job scratch directory root |
| `HOPPER_AUTOUPDATE` | _(off)_ | set to `1` to self-update from releases |
| `HOPPER_UPDATE_INTERVAL_MIN` | `60` | minutes between update checks |

## Tips

- **Generate tokens** with `openssl rand -hex 32`. Use *different* values for operator vs
  node so a leaked node token can't submit jobs.
- **Lease vs heartbeat:** the agent heartbeats every 30s; keep `HOPPER_LEASE_SECONDS`
  comfortably above that (default 120s) so brief network blips don't cause requeues.
- **Labels** are how you route work: GPU jobs to GPU nodes, big builds to big-memory nodes.
  A job is only claimed by a worker that advertises *all* the job's labels.
