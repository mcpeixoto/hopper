# Security & trust model

Read this before exposing Hopper to anything.

## The core assumption: a single trust domain

A worker runs **whatever Docker image and command the control plane hands it**, and Docker
runs effectively as root on the host. Therefore:

> Every worker node and the control plane must be machines **you** own and trust. Hopper is
> **not** a multi-tenant system. Submitting a job is equivalent to handing root on the
> chosen node to whoever submitted it.

This is the same trust model as a CI runner you self-host. It's perfectly safe for a personal
fleet (your laptop + your VPS + your spare box). It is **not** safe to let untrusted parties
submit jobs or to point a worker at someone else's control plane.

## What protects what

| Control | Protects against | Notes |
|---------|------------------|-------|
| `HOPPER_OPERATOR_TOKEN` | random people submitting jobs | required on any exposed server |
| `HOPPER_NODE_TOKEN` | random machines claiming your jobs | use a *different* value than the operator token |
| TLS (reverse proxy) | eavesdropping, tampering in transit | terminate in front of `hopperd` |
| `--network none` (default) | a job phoning home / exfiltrating | opt in with `HOPPER_ALLOW_NET=1` |
| `--cpus` / `--memory` | one job starving the node | set `HOPPER_CPU` / `HOPPER_MEM` |
| read-only input mount | a job tampering with its inputs | inputs at `/work/in:ro` |

The resource caps and `--network none` are **guardrails**, not a sandbox. A determined,
malicious image can still abuse a shared kernel. If you need to run untrusted code you need
a real sandbox — gVisor, Kata Containers, or Firecracker microVMs — which is out of scope.

## Hardening checklist

- [ ] Set **both** tokens to long random values (`openssl rand -hex 32`), different from each
      other. Never run an exposed server with empty tokens (the startup log warns you).
- [ ] Put `hopperd` behind TLS; don't serve the API over plaintext on the public internet.
- [ ] Keep `HOPPER_ALLOW_NET` off unless a job needs the network.
- [ ] Set CPU/memory caps so a runaway job can't take down the node.
- [ ] Run the agent as a dedicated user where possible; remember it still talks to the Docker
      socket, which is root-equivalent.
- [ ] Restrict the node console (`HOPPER_AGENT_ADDR`) to loopback (the default).
- [ ] Treat the control plane's data volume as sensitive — it holds job specs and artifacts.

## Reporting a vulnerability

Please open a private security advisory on the GitHub repository (Security → Report a
vulnerability) rather than a public issue.
