# Architecture

Hopper has two programs and one database.

```
                          ┌──────────────────────────── hopperd (control plane) ───────────────────────────┐
  operator / console ───▶ │  HTTP API            SQLite (the queue)            background reaper             │
        (operator token)  │  ├─ POST /api/jobs    ┌───────────────────────┐     ├─ requeue expired leases    │
                          │  ├─ GET  /api/jobs    │ jobs   (queued/in_… )  │     └─ mark stale/dead workers   │
                          │  ├─ POST /api/jobs/claim (long-poll)           │                                  │
                          │  ├─ POST /api/jobs/{id}/complete               │                                  │
                          │  └─ workers register / heartbeat               │                                  │
                          └───────────▲───────────────────────────────────────────────▲────────────────────┘
                                      │ outbound HTTPS (claim/complete/heartbeat)        │
            ┌─────────────────────────┴───────────┐                  ┌──────────────────┴───────────────────┐
            │ hopper-agent  (node A, behind NAT)   │                  │ hopper-agent (node B)                 │
            │  register → long-poll claim → run    │      …           │  …                                    │
            │  docker run <image> <command>        │                  │                                       │
            │  report exit code + logs, heartbeat  │                  │                                       │
            │  local status API → node console     │                  │                                       │
            └──────────────────────────────────────┘                  └───────────────────────────────────────┘
```

## Pull, not push

Workers open **outbound** connections to the control plane and **long-poll** for work. The
control plane never initiates a connection to a worker. This is the single most important
design decision, and it falls out of one constraint: **a worker (your laptop) is usually
behind NAT with no public inbound port.**

- A *push* design (server dials worker to hand it a job) needs a public IP + port-forward,
  or a reverse tunnel, or a VPN on every node — a second system that can break.
- A *pull* design needs only outbound HTTPS, which traverses NAT, captive portals, and
  dynamic IPs for free. The control plane just needs one public HTTPS endpoint.

Long-poll: the worker's `POST /api/jobs/claim` blocks on the server for up to
`HOPPER_LONGPOLL_SECONDS` (default 25s). If a job appears it returns immediately; otherwise
it returns `204` and the worker polls again. This gives near-instant dispatch without busy
polling.

## SQLite is the queue

There is no Redis/RabbitMQ/Celery. The `jobs` table, with a `status` column and an index on
the claim path, *is* a durable priority queue. A single writer connection
(`SetMaxOpenConns(1)`) plus WAL journalling serializes writes, so claims and completions are
atomic without application-level locking.

A claim is one guarded UPDATE:

```sql
UPDATE jobs SET status='in_flight', claimed_by=?, lease_expires_at=?, attempts=attempts+1
WHERE id=? AND status='queued';
```

The `status='queued'` guard means two workers can never claim the same job — the second
UPDATE affects zero rows and the worker moves on.

## Leases & the reaper (self-healing)

When a job is claimed it gets a **lease** (`lease_expires_at = now + HOPPER_LEASE_SECONDS`).
The worker renews the lease on every heartbeat while it runs. A background **reaper** on the
control plane periodically:

1. Requeues any `in_flight` job whose lease has expired (a node crashed, lost power, lost
   network) — incrementing `attempts`, or marking it `failed` once `max_attempts` is hit.
2. Marks workers that stopped heartbeating as `stale`, then `dead`.

This is the SQS-style **visibility timeout** pattern. It needs no broker — just a
`WHERE lease_expires_at < now` sweep — and it's what makes "my laptop went to sleep
mid-job" a non-event: the job simply runs again elsewhere.

## Job lifecycle

```
            submit            claim (lease)         complete(done)
  (none) ──────────▶ queued ───────────────▶ in_flight ───────────────▶ done
                       ▲                          │
                       │  complete(failed)        │ complete(failed) & attempts<max
                       └──────────────────────────┤
                                                  │ lease expires (reaper)
                                                  └──▶ queued  (or failed at max attempts)
  operator cancel ─────────────────────────────────▶ cancelled
```

## Execution model

The agent runs each job in its own container via the **docker CLI** (`os/exec`), not a
Docker SDK — the CLI is already on every node, so depending on it adds zero dependencies.
Each job gets a scratch dir with `in/` (mounted read-only) and `out/` (read-write), runs
with `--network none` by default and optional `--cpus`/`--memory` caps, and its combined
stdout/stderr + exit code are captured and reported.

## What Hopper deliberately is not

- **Not Kubernetes/Nomad.** Those schedule onto nodes the control plane can reach and bring
  a cluster to operate. Hopper inverts the connection direction and drops the cluster.
- **Not multi-tenant.** A worker runs whatever the control plane says, as root-ish. Every
  node must be yours. See [security.md](security.md).
- **Not a data pipeline engine.** No DAGs, no fan-out/fan-in (yet). It's a job queue.

## When you've outgrown it

Reach for a real scheduler when nodes are **created and destroyed by automation** (cloud
autoscaling) on a private network the control plane can address — roughly **>5 nodes** or
machine-managed lifecycle. Until then the pull queue is simpler and more robust.
