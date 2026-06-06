#!/usr/bin/env bash
# Point this node's Docker daemon at a Hopper pull-through registry cache.
#
#   MIRROR=https://registry.example.com ./scripts/setup-registry-mirror.sh
#
# It merges (not clobbers) "registry-mirrors" into /etc/docker/daemon.json and
# restarts Docker. Requires jq for a safe merge; falls back to a guarded create.
set -euo pipefail

MIRROR="${MIRROR:-${1:-}}"
DAEMON="${DAEMON_JSON:-/etc/docker/daemon.json}"
[ -n "$MIRROR" ] || { echo "usage: MIRROR=https://registry.example.com $0" >&2; exit 1; }

SUDO=""; [ "$(id -u)" -ne 0 ] && SUDO="sudo"
$SUDO mkdir -p "$(dirname "$DAEMON")"

if command -v jq >/dev/null 2>&1; then
  tmp="$(mktemp)"
  if [ -s "$DAEMON" ]; then
    $SUDO cat "$DAEMON"
  else
    echo '{}'
  fi | jq --arg m "$MIRROR" '.["registry-mirrors"] = ((.["registry-mirrors"] // []) + [$m] | unique)' > "$tmp"
  $SUDO cp "$tmp" "$DAEMON"
  rm -f "$tmp"
else
  if [ -s "$DAEMON" ]; then
    echo "jq not found and $DAEMON already exists — add this manually and restart docker:" >&2
    echo "  \"registry-mirrors\": [\"$MIRROR\"]" >&2
    exit 1
  fi
  $SUDO sh -c "printf '{\n  \"registry-mirrors\": [\"%s\"]\n}\n' '$MIRROR' > '$DAEMON'"
fi

echo "wrote $DAEMON with mirror $MIRROR"
if command -v systemctl >/dev/null 2>&1; then
  $SUDO systemctl restart docker && echo "docker restarted"
else
  echo "restart the Docker daemon to apply (no systemctl found)."
fi
echo "verify with: docker info | grep -A2 'Registry Mirrors'"
