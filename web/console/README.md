# Hopper console (operator GUI)

A dependency-free single-page app to submit jobs and watch the queue + node fleet. It talks
to a `hopperd` control plane using your **operator token** (entered in Settings, stored only
in your browser's localStorage).

## Run it

It's plain HTML/CSS/JS — any static server works:

```bash
# with the bundled Vite dev server
npm install && npm run dev        # http://localhost:5173

# or with zero install
python3 -m http.server 5173       # then open http://localhost:5173
```

Open Settings (⚙), set the control plane URL + operator token, and you're connected.

## Deploy

```bash
npm install && npm run build      # → dist/
```

Serve `dist/` from any static host (Caddy, nginx, Netlify, S3, GitHub Pages…). Two options:

1. **Same origin as the API** (recommended): proxy `/` → this static build and `/api` +
   `/health` → `hopperd`. No CORS needed; leave the URL field as the default (current origin).
2. **Separate origin:** host it anywhere and point it at `https://hopper.example.com`. Add
   that origin to the server's `HOPPER_CORS_ORIGINS`.

> The operator token grants job submission — only serve this console over HTTPS and to people
> you'd trust to run jobs on your nodes.
