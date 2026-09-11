#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$HOME/bin"
SERVICE="grant-store.service"
BINARY="grant-store"
UNIT_SRC="$REPO_DIR/$SERVICE"
UNIT_DEST="$HOME/.config/systemd/user/$SERVICE"

cd "$REPO_DIR"

export PATH="$HOME/.local/share/mise/shims:$PATH"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=${XDG_RUNTIME_DIR}/bus}"

echo "==> Testing..."
go test ./...

echo "==> Building $BINARY..."
go build -o "$BINARY" ./cmd/grant-store
echo "    built: $(ls -lh "$BINARY" | awk '{print $5}')"

echo "==> Installing systemd unit..."
mkdir -p "$(dirname "$UNIT_DEST")"
cp "$UNIT_SRC" "$UNIT_DEST"

echo "==> Stopping $SERVICE..."
systemctl --user stop "$SERVICE" 2>/dev/null || true
sleep 1

echo "==> Installing binary to $BIN_DIR..."
mkdir -p "$BIN_DIR"
cp "$BINARY" "$BIN_DIR/$BINARY"

echo "==> Starting $SERVICE..."
systemctl --user daemon-reload
systemctl --user enable "$SERVICE" >/dev/null
systemctl --user start "$SERVICE"

echo "==> Verifying..."
sleep 2
if systemctl --user is-active --quiet "$SERVICE"; then
  echo "    $SERVICE is running"
  journalctl --user -u "$SERVICE" -n 5 --no-pager 2>&1 | grep -v '^--' || true
else
  echo "ERROR: $SERVICE failed to start"
  journalctl --user -u "$SERVICE" -n 20 --no-pager 2>&1
  exit 1
fi

# A running process is not a working one: prove the API answers and that the
# vocabulary routes carry what the UI builds its pickers from.
echo "==> Smoke-checking the API..."
ADDR="${GRANT_STORE_ADDR:-127.0.0.1:8315}"
BASE="http://${ADDR}"
curl -sfS "$BASE/health" >/dev/null || { echo "ERROR: /health did not answer"; exit 1; }
curl -sfS "$BASE/relations" | grep -q '"can_use"' || { echo "ERROR: /relations did not list can_use"; exit 1; }
curl -sfS "$BASE/grants" >/dev/null || { echo "ERROR: /grants did not answer"; exit 1; }
echo "    /health, /relations and /grants answered"

# The bind is part of the contract: this service has no auth and must not be
# reachable off-host. Fail the deploy if it is listening anywhere else.
echo "==> Checking the bind..."
if ss -tlnH "sport = :${ADDR##*:}" | grep -q '127\.0\.0\.1:'; then
  echo "    bound to 127.0.0.1"
else
  echo "ERROR: grant-store is not bound to 127.0.0.1:"
  ss -tlnp "sport = :${ADDR##*:}"
  exit 1
fi

echo "==> Done."
