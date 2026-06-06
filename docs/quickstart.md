# Quickstart

Get a control plane and a worker running, then submit a job. ~5 minutes.

## Prerequisites

- **Go 1.22+** to build from source (or download binaries from
  [Releases](https://github.com/mcpeixoto/hopper/releases)).
- **Docker** on every machine that will run jobs (worker nodes).

## 1. Build

```bash
git clone https://github.com/mcpeixoto/hopper && cd hopper
make build        # produces ./bin/hopperd and ./bin/hopper-agent
```

## 2. Start the control plane

```bash
export HOPPER_OPERATOR_TOKEN=$(openssl rand -hex 32)
export HOPPER_NODE_TOKEN=$(openssl rand -hex 32)
./bin/hopperd
# → hopperd vX listening on :8080 (db=data/hopper.db)
```

Keep those two tokens — operators use the first, nodes use the second.

## 3. Start a worker

In another terminal (or on another machine):

```bash
export HOPPER_CONTROL_URL=http://localhost:8080   # the public URL on a real deployment
export HOPPER_NODE_TOKEN=<the node token from step 2>
export HOPPER_LABELS=cpu                           # optional capabilities
./bin/hopper-agent
# → agent registered as wrk_… ; node status API on http://127.0.0.1:8765
```

## 4. Submit a job

```bash
curl -s -X POST localhost:8080/api/jobs \
  -H "Authorization: Bearer $HOPPER_OPERATOR_TOKEN" \
  -d '{"image":"alpine:3.20","command":["echo","hello from hopper"]}'
```

Note the returned `id`, then poll it:

```bash
curl -s localhost:8080/api/jobs/<id> -H "Authorization: Bearer $HOPPER_OPERATOR_TOKEN"
# status moves: queued → in_flight → done, with exit_code and logs reference
```

Check the node console JSON to see what the worker did:

```bash
curl -s localhost:8765/api/status
```

## 5. Try a job that needs a capability

```bash
# This job requires the "gpu" label; a cpu-only worker will not claim it.
curl -s -X POST localhost:8080/api/jobs \
  -H "Authorization: Bearer $HOPPER_OPERATOR_TOKEN" \
  -d '{"image":"alpine:3.20","command":["nvidia-smi"],"labels":["gpu"]}'
```

Start a worker with `HOPPER_LABELS=gpu` and it'll get picked up.

## Next

- Put `hopperd` behind TLS and onboard a real remote node → [client.md](client.md)
- Enable self-updating releases → [releases.md](releases.md)
- Understand the trust model before exposing it → [security.md](security.md)
