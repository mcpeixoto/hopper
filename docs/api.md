# HTTP API

Base URL is the control plane, e.g. `https://hopper.example.com`. All `/api/*` routes
require a bearer token; `/health` does not.

- **Operator plane** (submit/admin) → `Authorization: Bearer <HOPPER_OPERATOR_TOKEN>`
- **Worker plane** (claim/complete/register) → `Authorization: Bearer <HOPPER_NODE_TOKEN>`

The two tokens are distinct roles: an operator token cannot claim jobs, and a node token
cannot submit them. If a token is unset on the server, that plane's auth is **disabled**
(dev mode) — the server logs a warning at startup.

## Health

### `GET /health`
No auth. → `200 {"status":"ok","ts":"..."}`

### `GET /metrics`
No auth. Prometheus text format: cumulative counters
(`hopper_jobs_submitted_total`, `…_claimed_total`, `…_completed_total`, `…_failed_total`)
plus gauges (`hopper_jobs{status="…"}`, `hopper_workers{status="…"}`). Proxy it privately
if the numbers are sensitive.

## Operator plane

### `POST /api/jobs` — submit a job
Body:
```json
{
  "image": "alpine:3.20",          // required
  "command": ["echo", "hi"],        // optional argv
  "env": {"KEY": "VALUE"},          // optional container env
  "labels": ["gpu"],                // optional required node capabilities
  "priority": 0,                     // higher = claimed first
  "timeout_s": 3600,                 // default 3600
  "max_attempts": 3,                 // default 3
  "submitted_by": "ci",              // optional provenance tag
  "paused": false                    // create paused so an input can be attached first
}
```
→ `201` with the created job object.

**Jobs with inputs** must be created `paused` so a worker can't claim them before the
input is attached. The flow is: submit with `"paused": true` → `PUT .../input` →
`POST .../release`.

### `POST /api/jobs/{id}/release` — release a paused job
Moves a `paused` job to `queued`. → `200 {"ok":true}` or `404` if no paused job.

### `PUT /api/jobs/{id}/input` — attach an input artifact
Body: a **tar.gz** of files; the worker unpacks it into `/work/in` (read-only) before the
run. → `201` with the artifact (`id`, `content_hash`, `size_bytes`).

### `GET /api/jobs/{id}/result` — download the output
A **tar.gz** of the job's `/work/out`. → `200` (gzip stream) when `done`; `204` if the job
produced no output; `202` while pending; `409` if failed/cancelled.

### `GET /api/jobs/{id}/logs` — download captured logs
Combined stdout/stderr as text. → `200 text/plain`, or `404` if the job produced none.

### `GET /api/jobs` — list jobs / history
Filters (combinable): `?status=`, `?image=` (substring), `?submitted_by=` (substring),
`?since=` (RFC3339, `created_at >=`), `?limit=`. → `200 [job, …]` (newest first).

### `GET /api/jobs/{id}` — get one job
→ `200 job` or `404`.

### `POST /api/jobs/{id}/cancel` — cancel a job
→ `200 {"ok":true}` or `404`.

### `GET /api/workers` — list the node fleet
→ `200 [worker, …]` with `status` ∈ `online|stale|dead|draining` and `last_heartbeat`.

## Worker plane

### `POST /api/workers/register`
Body `{"hostname":"laptop","labels":["cpu"]}`. Idempotent by hostname. → `200 worker`.

### `POST /api/workers/{id}/heartbeat`
Refreshes liveness and renews the lease on the worker's in-flight job. → `200 {"ok":true}`.

### `POST /api/jobs/claim` — long-poll for work
Body `{"worker_id":"wrk_…","labels":["cpu"]}`. Blocks up to `HOPPER_LONGPOLL_SECONDS`.
→ `200 job` (now `in_flight`, leased) or `204` if nothing claimable. A job is only offered
to a worker whose labels satisfy **all** of the job's required labels.

### `GET /api/jobs/{id}/input` — download the input blob
The worker fetches the tar.gz attached by the operator. → `200` gzip, or `404` if none.

### `PUT /api/jobs/{id}/output` — upload the output blob
The worker uploads a tar.gz of `/work/out`. → `201` with the artifact id.

### `PUT /api/jobs/{id}/logs` — upload captured logs
→ `201` with the artifact id.

### `POST /api/jobs/{id}/complete` — report result
Body:
```json
{ "status": "done", "exit_code": 0, "output_artifact_id": "art_…", "logs_ref": "art_…", "error": "" }
```
`status` is `done` or `failed`. `output_artifact_id`/`logs_ref` are the ids returned by the
output/logs uploads (omit if none). A `failed` job with attempts remaining is automatically
**requeued**; once `max_attempts` is reached it stays `failed`. → `200 {"ok":true}`.

## The job object

```json
{
  "id": "job_6327ae34180f",
  "image": "alpine:3.20",
  "command": ["echo", "hi"],
  "env": {},
  "labels": [],
  "priority": 0,
  "status": "done",
  "claimed_by": "wrk_d8cd79c4f299",
  "attempts": 1,
  "max_attempts": 3,
  "timeout_s": 60,
  "exit_code": 0,
  "error": "",
  "created_at": "2026-06-06T21:14:13Z",
  "updated_at": "2026-06-06T21:14:17Z"
}
```

## Go client

The repo ships a typed client at `internal/client` used by the agent; you can use it from Go
tooling too:

```go
c := client.New("https://hopper.example.com", operatorToken)
job, err := c.SubmitJob(ctx, client.JobSpec{Image: "alpine:3.20", Command: []string{"echo", "hi"}})
```
