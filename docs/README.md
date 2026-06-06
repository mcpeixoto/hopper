# Hopper documentation

Hopper is a tiny pull-based distributed job dispatcher: submit a Docker job to a control
plane, and any free worker node claims it, runs it, and reports back.

## Start here

- **[Quickstart](quickstart.md)** — run a control plane + worker and submit your first job
- **[Architecture](architecture.md)** — how it works and why it's built this way

## Operating

- **[Control plane (`hopperd`)](server.md)** — running and deploying the server
- **[Worker nodes (`hopper-agent`)](client.md)** — onboarding and running nodes
- **[Operator CLI (`hopper`)](cli.md)** — submit and manage jobs from the terminal
- **[Configuration reference](configuration.md)** — every `HOPPER_*` variable
- **[Releases & auto-update](releases.md)** — tag-driven releases and self-updating fleets
- **[GitHub Actions runners](github-actions.md)** — run your CI on the fleet
- **[Security & trust model](security.md)** — the single-trust-domain assumption, hardening

## Reference

- **[HTTP API](api.md)** — endpoints, payloads, status codes
- **[Roadmap](roadmap.md)** — distributed GitHub Actions runners, Docker cache, artifacts

## The 60-second mental model

```
submit ─▶ hopperd (queue in SQLite) ─▶ worker long-polls & claims ─▶ docker run ─▶ result
                  ▲                                                                    │
                  └──────────────────── reaper requeues if the node dies ◀────────────┘
```

- Workers **pull** (outbound HTTPS), so NAT is a non-issue.
- A claim grants a **lease**; a missed lease means the job is requeued.
- State is one SQLite file. No broker, no Kubernetes.
