# Roadmap

Hopper's core (queue, pull-based dispatch, leases, dual GUIs, releases, auto-update) is
built. These are the planned next steps, each of which fits the existing model cleanly.

## ✅ Artifact handoff (job inputs & outputs) — shipped

Jobs can now take inputs and return outputs, not just logs:

- Submit `paused`, `PUT .../input` a tar.gz, `POST .../release`; the agent unpacks it into
  `/work/in` before running.
- The agent tars `/work/out` and uploads it; operators fetch it via `GET /api/jobs/{id}/result`
  and logs via `GET /api/jobs/{id}/logs`.
- Content-addressed blob store (SHA-256 names → free dedup + integrity); blobs stream
  through the control plane (no extra service).

**Still to do:** graduate to MinIO/S3 + presigned URLs when blobs routinely exceed ~100 MB
or a third node makes the control plane's uplink the bottleneck.

## ✅ Distributed GitHub Actions runners — shipped

Hopper runs your CI on your own fleet as **ephemeral self-hosted runners** — see
[github-actions.md](github-actions.md). A `workflow_job:queued` webhook (HMAC-verified) makes
Hopper mint a JIT runner token and enqueue a job that runs the runner image in `--ephemeral`
mode on a CI-labelled node.

**Still to do:** Docker-in-Docker / socket mount so container-based steps and `services:`
work (v1 runs plain steps and most actions); GitHub App auth as an alternative to a PAT.

## ✅ Docker image cache — shipped

See [docker-cache.md](docker-cache.md). Per-node caching via `HOPPER_PULL_POLICY`, plus a
ready-to-run `registry:2` pull-through cache (`deploy/registry/`) and a node setup helper
(`scripts/setup-registry-mirror.sh`). **Still to do:** BuildKit cache for image-building jobs.

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
