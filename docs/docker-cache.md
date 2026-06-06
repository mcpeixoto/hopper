# Docker image cache

Two layers, cheapest first. You probably want both eventually; start with #1.

## 1. Per-node cache (free, already on)

Each node's Docker daemon caches pulled layers, so re-running the same image skips the pull.
The agent honours `HOPPER_PULL_POLICY`:

- `if-not-present` (default) — only pull when the image is absent. Fast re-runs.
- `always` — `docker pull` before every run. Use when a mutable tag (e.g. `:latest`) must be
  re-checked each time.

That's all most single-fleet setups need.

## 2. Shared pull-through cache (the real win at scale)

Run one `registry:2` configured as a Docker Hub pull-through cache near the fleet. The first
node to need an image warms the cache; every other node pulls it over the LAN/VPS instead of
the internet — and you **stop hitting Docker Hub's anonymous pull rate limits**, which bite
hard once nodes are ephemeral (CI runners) or numerous.

### Run the cache

```bash
cd deploy/registry
docker compose up -d        # listens on :5000
```

Optionally add Docker Hub credentials in `deploy/registry/config.yml` to raise upstream
limits, and front it with TLS if nodes reach it over the internet.

### Point nodes at it

```bash
MIRROR=https://registry.example.com ./scripts/setup-registry-mirror.sh
```

This merges `registry-mirrors` into `/etc/docker/daemon.json` and restarts Docker. Verify:

```bash
docker info | grep -A2 'Registry Mirrors'
```

Now `docker pull alpine` (and every Hopper job pulling Hub images) goes through your cache.

> A registry mirror caches **pulls** of Docker Hub images. If your jobs **build** images,
> use BuildKit's `--cache-to`/`--cache-from` against a registry instead (separate concern).
> Images from other registries (GHCR, etc.) aren't mirrored by this Hub-only cache.
