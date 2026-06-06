# Hopper node console (client GUI)

The per-node dashboard: it shows what *this* machine is doing — state, current job, jobs
done/failed, version, and the last job's output.

Unlike the operator [console](../console), this GUI is **embedded into the `hopper-agent`
binary** (via `go:embed` in `embed.go`) and served by the agent itself on its loopback
address (`HOPPER_AGENT_ADDR`, default `127.0.0.1:8765`). No build step, no separate deploy —
it ships inside the agent.

## Use it

With an agent running, open:

```
http://127.0.0.1:8765/
```

It polls the agent's local `GET /api/status` every couple of seconds. Because it binds to
loopback, it's private to that machine.

## Editing

The page is plain HTML/CSS/JS (`index.html`, `app.js`, `styles.css`). Edit and rebuild the
agent (`make agent`) — the new assets are embedded automatically. To iterate without
rebuilding, serve this folder statically and point it at a running agent's `/api/status`
(adjust the fetch URL).
