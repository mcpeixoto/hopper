#!/usr/bin/env bash
# Hopper worker onboarding — one command to turn a machine into a worker node.
#
#   curl -fsSL https://raw.githubusercontent.com/mcpeixoto/hopper/main/scripts/install-worker.sh | \
#     HOPPER_CONTROL_URL=https://hopper.example.com HOPPER_NODE_TOKEN=xxxx HOPPER_LABELS=cpu bash
#
# Required env: HOPPER_CONTROL_URL, HOPPER_NODE_TOKEN
# Optional env: HOPPER_LABELS, HOPPER_AUTOUPDATE, HOPPER_VERSION (default: latest), REPO
set -euo pipefail

REPO="${REPO:-mcpeixoto/hopper}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
ENV_FILE="${ENV_FILE:-/etc/hopper/agent.env}"

err() { echo "error: $*" >&2; exit 1; }
info() { echo ">> $*"; }

[ -n "${HOPPER_CONTROL_URL:-}" ] || err "set HOPPER_CONTROL_URL"
[ -n "${HOPPER_NODE_TOKEN:-}" ] || err "set HOPPER_NODE_TOKEN"

# --- prerequisites ---
command -v docker >/dev/null 2>&1 || err "docker is not installed"
docker info >/dev/null 2>&1 || err "docker daemon not reachable (is it running? are you in the docker group?)"

# --- detect platform ---
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in linux|darwin) ;; *) err "unsupported OS: $os" ;; esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported arch: $arch" ;;
esac
asset="hopper-agent_${os}_${arch}"

# --- resolve version ---
version="${HOPPER_VERSION:-latest}"
if [ "$version" = "latest" ]; then
  info "resolving latest release"
  version="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -1 | cut -d'"' -f4)"
  [ -n "$version" ] || err "could not resolve latest release; set HOPPER_VERSION"
fi
info "installing hopper-agent ${version} (${os}/${arch})"

# --- download binary + verify checksum ---
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
base="https://github.com/${REPO}/releases/download/${version}"
curl -fSL "${base}/${asset}" -o "$tmp/hopper-agent"
if curl -fsSL "${base}/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  want="$(grep " ${asset}\$" "$tmp/checksums.txt" | awk '{print $1}')"
  if [ -n "$want" ] && command -v sha256sum >/dev/null 2>&1; then
    got="$(sha256sum "$tmp/hopper-agent" | awk '{print $1}')"
    [ "$want" = "$got" ] || err "checksum mismatch for ${asset}"
    info "checksum verified"
  fi
fi
chmod +x "$tmp/hopper-agent"

# --- install binary ---
SUDO=""
[ "$(id -u)" -ne 0 ] && SUDO="sudo"
$SUDO install -m 0755 "$tmp/hopper-agent" "${BIN_DIR}/hopper-agent"
info "installed ${BIN_DIR}/hopper-agent"

# --- write env file (mode 600; holds the node token) ---
$SUDO mkdir -p "$(dirname "$ENV_FILE")"
$SUDO sh -c "umask 177; cat > '$ENV_FILE'" <<EOF
HOPPER_CONTROL_URL=${HOPPER_CONTROL_URL}
HOPPER_NODE_TOKEN=${HOPPER_NODE_TOKEN}
HOPPER_LABELS=${HOPPER_LABELS:-}
HOPPER_AUTOUPDATE=${HOPPER_AUTOUPDATE:-}
EOF
info "wrote ${ENV_FILE} (mode 600)"

# --- service: systemd if available, otherwise print a fallback ---
if command -v systemctl >/dev/null 2>&1 && [ "$os" = linux ]; then
  unit=/etc/systemd/system/hopper-agent.service
  $SUDO sh -c "cat > '$unit'" <<EOF
[Unit]
Description=Hopper worker agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN_DIR}/hopper-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now hopper-agent
  info "hopper-agent service started — follow logs with: journalctl -u hopper-agent -f"
else
  cat <<EOF

systemd not found — start the agent manually or via your init system:

  set -a; . ${ENV_FILE}; set +a
  ${BIN_DIR}/hopper-agent

On macOS, create a launchd plist, or run under a supervisor of your choice.
EOF
fi
