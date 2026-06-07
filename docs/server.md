# Control plane (`hopperd`)

`hopperd` is the always-on server: it holds the job queue, the worker registry, the artifact
store, and runs the lease reaper. Run it on a machine your worker nodes can reach over
HTTPS (a VPS, a home server with a tunnel, etc.).

## Run from source

```bash
export HOPPER_OPERATOR_TOKEN=$(openssl rand -hex 32)
export HOPPER_NODE_TOKEN=$(openssl rand -hex 32)
make run-server      # or: ./bin/hopperd
```

## Run with Docker

```bash
cp .env.example .env     # set HOPPER_OPERATOR_TOKEN and HOPPER_NODE_TOKEN
docker compose up -d
```

This builds (or pulls `ghcr.io/mcpeixoto/hopper`) and runs `hopperd` on `:8080` with a named
volume for the SQLite DB and artifacts.

## Put it behind TLS

`hopperd` speaks plain HTTP; terminate TLS with a reverse proxy so nodes can reach it at a
public `https://` hostname. Any proxy works — Caddy is the least effort:

```caddyfile
hopper.example.com {
    reverse_proxy localhost:8080
}
```

With nginx/Traefik/nginx-proxy-manager, proxy `hopper.example.com → hopperd:8080` and enable
a Let's Encrypt cert. Workers only ever make **outbound** HTTPS to this host.

If you also serve the [web console](../web/console), route `/` to the console's static build
and `/api` + `/health` to `hopperd` on the same origin (so no CORS needed); otherwise add
the console's origin to `HOPPER_CORS_ORIGINS`.

## Storage: SQLite or Postgres

By default `hopperd` stores everything in a single SQLite file (`HOPPER_DB_PATH`) — zero
setup, perfect for one control plane. To use **Postgres** instead, point `HOPPER_DB_PATH` at
a DSN:

```bash
HOPPER_DB_PATH=postgres://user:pass@db.internal:5432/hopper?sslmode=require
```

The same schema is applied automatically on either backend (queries are placeholder-rewritten
for Postgres). Reach for Postgres when you want managed backups/HA or you've outgrown a single
SQLite writer; otherwise SQLite is simpler and plenty fast for a control plane.

## Operating notes

- **Backups:** everything is in `HOPPER_DB_PATH` (+ `HOPPER_ARTIFACT_DIR`). Back up the data
  volume; that's the entire state.
- **Restarts are safe:** in-flight jobs keep their lease; if `hopperd` is down past a lease
  the job is requeued on next startup by the reaper. Workers retry registration with backoff.
- **Scaling:** one `hopperd` handles far more than a handful of nodes. SQLite's single
  writer is the limit; you'll hit node-count reasons to switch schedulers long before it.
- **Auth:** never run a publicly reachable `hopperd` with empty tokens. The startup log
  warns when auth is disabled.

## Health & monitoring

`GET /health` returns `200 {"status":"ok"}` — wire it to your uptime check. `GET /api/workers`
(operator token) shows the fleet with `last_heartbeat` and `online|stale|dead` status.
