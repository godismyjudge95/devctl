#!/usr/bin/env bash
# scripts/test-env-orb.sh — OrbStack Ubuntu VM lifecycle for Linux integration tests.
#
# Mirrors scripts/test-env.sh. Use this on macOS when Incus is not installed.
# The host Darwin daemon is never used. Tests run inside the Ubuntu machine.
#
# Requires: OrbStack.app running, orbctl, a Linux GOOS/GOARCH binary (this
# script builds linux/arm64 when ./devctl is a Mach-O file).
set -euo pipefail

if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null && [ "$(tput colors)" -ge 8 ]; then
  GREEN="$(tput setaf 2)"; RED="$(tput setaf 1)"; CYAN="$(tput setaf 6)"; RESET="$(tput sgr0)"
else
  GREEN="" RED="" CYAN="" RESET=""
fi
info()    { printf '%s→ %s%s\n' "${CYAN}"  "$*" "${RESET}"; }
success() { printf '%s✓ %s%s\n' "${GREEN}" "$*" "${RESET}"; }
error()   { printf '%s✗ %s%s\n' "${RED}"   "$*" "${RESET}" >&2; }

MODE="interactive"
if [[ "${1:-}" == "--run-tests" ]]; then
  MODE="run-tests"
fi

if ! command -v orbctl >/dev/null 2>&1; then
  error "OrbStack (orbctl) is not installed."
  exit 1
fi

BIN="./devctl"
if [[ ! -f "$BIN" ]] || file "$BIN" | grep -q 'Mach-O'; then
  info "Building linux/arm64 binary for the OrbStack Ubuntu machine..."
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION:-dev}" -o /tmp/devctl.linux .
  BIN="/tmp/devctl.linux"
fi
if [[ ! -f "$BIN" ]]; then
  error "Linux binary not found — run 'make build' or allow this script to cross-compile."
  exit 1
fi

CONTAINER="devctl-test-$(date +%s)"

cleanup() {
  echo ""
  bash "$(dirname "$0")/test-cleanup.sh" "$CONTAINER"
}
trap cleanup EXIT

info "Creating OrbStack machine ${CONTAINER} (ubuntu:24.04)..."
orbctl create --memory 4G --cpus 2 --disk 32G ubuntu:24.04 "$CONTAINER"
success "Machine created."

orb_exec() {
  orb run -m "$CONTAINER" sudo "$@"
}

info "Waiting for systemd..."
TIMEOUT=60
ELAPSED=0
while true; do
  STATE="$(orb_exec systemctl is-system-running 2>/dev/null || true)"
  if [[ "$STATE" == "running" || "$STATE" == "degraded" ]]; then
    success "Systemd is ready (state: ${STATE})."
    break
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    error "Timed out waiting for systemd (last state: '${STATE}')."
    exit 1
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

info "Installing curl jq git..."
orb_exec apt-get update -qq
orb_exec apt-get install -y -qq curl jq git
success "Base packages installed."

info "Pushing linux devctl binary..."
ABS_BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
orb_exec cp "$ABS_BIN" /usr/local/bin/devctl
orb_exec chmod 755 /usr/local/bin/devctl
success "Binary installed."

info "Creating testuser..."
orb_exec useradd -m testuser || true
orb_exec mkdir -p /home/testuser/ddev/sites/server
orb_exec chown -R testuser:testuser /home/testuser/ddev
orb_exec chown testuser:testuser /usr/local/bin/devctl
success "testuser created."

info "Writing PHP 8.4 stub..."
PHP_DIR="/home/testuser/ddev/sites/server/php/8.4"
orb_exec mkdir -p "$PHP_DIR"
orb_exec bash -c "echo '#!/bin/sh' > ${PHP_DIR}/php-fpm && chmod 755 ${PHP_DIR}/php-fpm"
orb_exec tee "${PHP_DIR}/php.ini" >/dev/null <<'PHPINI'
memory_limit = 128M
max_execution_time = 30
upload_max_filesize = 2M
post_max_size = 8M
PHPINI
orb_exec chown -R testuser:testuser /home/testuser/ddev

info "Writing systemd unit..."
orb_exec tee /etc/systemd/system/devctl.service >/dev/null <<'EOF'
[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
User=testuser
Group=testuser
NoNewPrivileges=true
ExecStart=/usr/local/bin/devctl daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server
Environment=DEVCTL_TESTING=true

[Install]
WantedBy=multi-user.target
EOF

info "Writing elevate systemd unit..."
orb_exec tee /etc/systemd/system/devctl-elevate.service >/dev/null <<'EOF'
[Unit]
Description=devctl elevated bind supervisor
After=network.target
Before=devctl.service

[Service]
Type=simple
User=testuser
Group=testuser
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/local/bin/devctl elevate daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server
Environment=DEVCTL_ELEVATED=1
Environment=DEVCTL_TESTING=true

[Install]
WantedBy=multi-user.target
EOF

info "Starting devctl..."
orb_exec systemctl daemon-reload
orb_exec systemctl enable devctl-elevate
orb_exec systemctl enable devctl
orb_exec systemctl start devctl-elevate
orb_exec systemctl start devctl

info "Waiting for dashboard..."
TIMEOUT=45
ELAPSED=0
while true; do
  if orb_exec curl -sf http://127.0.0.1:4000/api/settings/resolved >/dev/null 2>&1; then
    success "devctl is responding inside ${CONTAINER}."
    break
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    error "Timed out waiting for devctl HTTP API."
    orb_exec journalctl -u devctl -n 30 --no-pager || true
    exit 1
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

export DEVCTL_CONTAINER="$CONTAINER"
success "OrbStack test machine ${CONTAINER} is ready."
echo "export DEVCTL_CONTAINER=${CONTAINER}"

if [[ "$MODE" == "run-tests" ]]; then
  info "Running API tests..."
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=integration -o /tmp/devctl.api.test ./tests/api/
  orb_exec cp /tmp/devctl.api.test /tmp/devctl.test
  orb_exec chmod 755 /tmp/devctl.test
  orb_exec env DEVCTL_BASE_URL=http://127.0.0.1:4000 DEVCTL_SITE_USER=testuser /tmp/devctl.test -test.v
fi

if [[ "$MODE" == "interactive" ]]; then
  info "Machine will stay up until Ctrl+C."
  while true; do sleep 3600; done
fi
