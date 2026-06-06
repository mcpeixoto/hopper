# Roadmap

Hopper's core (queue, pull-based dispatch, leases, dual GUIs, releases, auto-update) is
built. These are the planned next steps, each of which fits the existing model cleanly.

## Artifact handoff (job inputs & outputs)

Today a job runs a command and Hopper captures its exit code + logs. Next: ship **input
blobs** to the node and retrieve **output blobs**.

- Upload an input artifact on submit; the agent downloads it to `/work/in` before running.
- The agent tars `/work/out` and uploads it on completion; operators fetch it via
  `GET /api/jobs/{id}/result`.
- Integrity via SHA-256; blobs stream through the control plane (no extra service).
- Graduate to MinIO/S3 + presigned URLs when blobs routinely exceed ~100 MB or a third node
  makes the control plane's uplink the bottleneck.

## Distributed GitHub Actions runners

Hopper is a natural fit for **ephemeral self-hosted runners** — think
`actions-runner-controller`, minus Kubernetes.

- A GitHub App lets Hopper mint just-in-time runner tokens.
- `hopperd` exposes a webhook; on a `workflow_job` *queued* event for
  `runs-on: [self-hosted, hopper]`, it submits a job running the official runner image in
  `--ephemeral` mode.
- A free node claims it, the runner executes exactly one workflow run, then the container is
  torn down. No long-lived runner to patch.

This reuses the whole queue/claim/lease/reaper machinery; the only new pieces are the webhook
receiver and a thin GitHub API client. The trust caveat still applies — only your own repos.

## Docker image cache

- **Per-node:** the agent already avoids re-pulling present images (`if-not-present` policy).
- **Shared:** run a `registry:2` pull-through cache near the fleet and point each node's
  Docker daemon at it via `--registry-mirror`. First pull warms the cache; the rest come over
  the LAN, dodging Docker Hub rate limits (which bite once nodes are ephemeral).

## Signed releases

Add minisign/cosign signatures to release artifacts and verify them in the updater before
replacing a binary — closing the gap noted in [releases.md](releases.md).

## Smaller items

- Operator CLI (`hopper submit`, `hopper jobs`, `hopper nodes`) built on `internal/client`.
- Job logs as a first-class artifact with `GET /api/jobs/{id}/logs` streaming.
- Server-advertised target version so agents converge to the server's version, not just the
  latest tag.
- Optional Postgres backend for the control plane (same DDL) if SQLite's single writer ever
  becomes the bottleneck.

Have an idea? Open a [feature request](https://github.com/mcpeixoto/hopper/issues/new).
