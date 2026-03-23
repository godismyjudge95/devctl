#!/usr/bin/env bash
# scripts/test-env.sh — Incus container lifecycle for devctl testing
# Make this file executable: chmod +x scripts/test-env.sh
set -euo pipefail

# ─── Colour helpers ────────────────────────────────────────────────────────────
if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null && [ "$(tput colors)" -ge 8 ]; then
  GREEN="$(tput setaf 2)"
  RED="$(tput setaf 1)"
  CYAN="$(tput setaf 6)"
  BOLD="$(tput bold)"
  RESET="$(tput sgr0)"
else
  GREEN="" RED="" CYAN="" BOLD="" RESET=""
fi

info()    { printf '%s→ %s%s\n' "${CYAN}"  "$*" "${RESET}"; }
success() { printf '%s✓ %s%s\n' "${GREEN}" "$*" "${RESET}"; }
error()   { printf '%s✗ %s%s\n' "${RED}"   "$*" "${RESET}" >&2; }

# ─── Parse arguments ───────────────────────────────────────────────────────────
MODE="interactive"
if [[ "${1:-}" == "--run-tests" ]]; then
  MODE="run-tests"
fi

# ─── Step 1: Check binary ──────────────────────────────────────────────────────
if [[ ! -f "./devctl" ]]; then
  error "Binary ./devctl not found — run 'make build' first."
  exit 1
fi

# ─── Step 2: Check incus ──────────────────────────────────────────────────────
if ! command -v incus &>/dev/null; then
  error "Incus is not installed. Install it from: https://linuxcontainers.org/incus/docs/main/installing/"
  exit 1
fi

# ─── Step 2b: Ensure container forwarding rules (WSL2 + Docker compat) ────────
# Docker sets FORWARD policy to DROP. On WSL2 with mirrored networking this
# blocks Incus container egress. Inject accept rules for incusbr0 if missing.
if ! iptables -C FORWARD -i incusbr0 -j ACCEPT 2>/dev/null; then
  info "Adding iptables FORWARD rules for incusbr0 (WSL2 + Docker compat)..."
  iptables -I FORWARD 1 -i incusbr0 -j ACCEPT
  iptables -I FORWARD 2 -o incusbr0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  success "Forwarding rules added."
fi

# ─── Step 3: Generate container name ─────────────────────────────────────────
CONTAINER="devctl-test-$(date +%s)"

# ─── Track whether we stopped the system devctl service ──────────────────────
STOPPED_SYSTEM_DEVCTL=0

# ─── EXIT trap (set before launch so it always fires) ─────────────────────────
cleanup() {
  echo ""
  echo "Destroying container ${CONTAINER}..."
  incus delete --force "$CONTAINER" 2>/dev/null || true
  if [[ "$STOPPED_SYSTEM_DEVCTL" -eq 1 ]]; then
    info "Restarting system devctl service..."
    systemctl start devctl 2>/dev/null || true
    success "System devctl restarted."
  fi
}
trap cleanup EXIT

# ─── Step 4: Launch container ─────────────────────────────────────────────────
info "Launching container ${CONTAINER}..."
if incus launch devctl-ubuntu-base "$CONTAINER" 2>/dev/null; then
  success "Container launched from cached image 'devctl-ubuntu-base'."
else
  info "Local image 'devctl-ubuntu-base' not found — falling back to images:ubuntu/24.04..."
  incus launch images:ubuntu/24.04 "$CONTAINER"
  success "Container launched from images:ubuntu/24.04."
fi

# ─── Step 5: Wait for systemd ─────────────────────────────────────────────────
info "Waiting for systemd to be ready..."
TIMEOUT=30
ELAPSED=0
while true; do
  STATE="$(incus exec "$CONTAINER" -- systemctl is-system-running 2>/dev/null || true)"
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

# ─── Step 6: Push binary ──────────────────────────────────────────────────────
info "Pushing devctl binary..."
incus file push ./devctl "$CONTAINER/usr/local/bin/devctl"
incus exec "$CONTAINER" -- chmod 755 /usr/local/bin/devctl
success "Binary pushed to /usr/local/bin/devctl."

# ─── Step 7: Create testuser ──────────────────────────────────────────────────
info "Creating testuser..."
incus exec "$CONTAINER" -- useradd -m testuser
incus exec "$CONTAINER" -- mkdir -p /home/testuser/ddev/sites/server
incus exec "$CONTAINER" -- chown -R testuser:testuser /home/testuser/ddev
success "testuser created."

# ─── Step 7b: Mount artifact cache volume ─────────────────────────────────────
ARTIFACTS_POOL="default"
ARTIFACTS_VOLUME="devctl-test-artifacts"
ARTIFACTS_MOUNT="/var/cache/devctl-artifacts"

if incus storage volume show "$ARTIFACTS_POOL" "$ARTIFACTS_VOLUME" &>/dev/null; then
  info "Mounting artifact cache volume into container..."
  incus config device add "$CONTAINER" artifacts disk \
    pool="$ARTIFACTS_POOL" \
    source="$ARTIFACTS_VOLUME" \
    path="$ARTIFACTS_MOUNT"
  success "Artifact cache mounted at ${ARTIFACTS_MOUNT}."
else
  info "Artifact cache volume '${ARTIFACTS_VOLUME}' not found — installs will download from the internet."
  info "Run 'sudo make test-artifacts-download' after 'sudo make test-env-setup' to enable caching."
  ARTIFACTS_MOUNT=""
fi

# ─── Step 7c: Install curl shim (serves cached files instead of downloading) ──
if [[ -n "$ARTIFACTS_MOUNT" ]]; then
  info "Installing curl cache shim..."
  incus exec "$CONTAINER" -- tee /usr/local/bin/curl >/dev/null <<'CURLSHIM'
#!/bin/bash
# curl shim — serve files from the local artifact cache when available.
# Falls through to the real curl for everything else (version checks, etc.)
CACHE_DIR="/var/cache/devctl-artifacts"
REAL_CURL="/usr/bin/curl"

# Parse -o DEST from the argument list (devctl always uses: curl -fsSL -o <dest> <url>)
DEST=""
PREV=""
for arg in "$@"; do
  if [[ "$PREV" == "-o" ]]; then
    DEST="$arg"
  fi
  PREV="$arg"
done

if [[ -n "$DEST" ]]; then
  BASENAME="$(basename "$DEST")"
  CACHED="${CACHE_DIR}/${BASENAME}"
  if [[ -f "$CACHED" ]]; then
    cp "$CACHED" "$DEST"
    echo "curl-shim: served ${BASENAME} from cache" >&2
    exit 0
  fi
fi

exec "$REAL_CURL" "$@"
CURLSHIM
  incus exec "$CONTAINER" -- chmod 755 /usr/local/bin/curl
  success "curl shim installed at /usr/local/bin/curl."
fi

# ─── Step 8: Write service unit ───────────────────────────────────────────────
info "Writing devctl.service unit..."
incus exec "$CONTAINER" -- tee /etc/systemd/system/devctl.service >/dev/null <<'EOF'
[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/devctl
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server

[Install]
WantedBy=multi-user.target
EOF
success "Service unit written."

# ─── Step 9: Enable and start devctl ─────────────────────────────────────────
info "Enabling and starting devctl service..."
incus exec "$CONTAINER" -- systemctl daemon-reload
incus exec "$CONTAINER" -- systemctl enable devctl
incus exec "$CONTAINER" -- systemctl start devctl
success "devctl service started."

# ─── Step 10: Free port 4000 if system devctl is using it ────────────────────
if ss -tlnp 2>/dev/null | grep -q '127.0.0.1:4000\|0\.0\.0\.0:4000\|\*:4000'; then
  info "Port 4000 is in use — stopping system devctl service..."
  systemctl stop devctl 2>/dev/null || true
  STOPPED_SYSTEM_DEVCTL=1
  # Wait a moment for the port to be released
  sleep 1
  success "System devctl stopped."
fi

# ─── Step 11: Add port proxy ─────────────────────────────────────────────────
info "Adding port proxy (container:4000 → host:4000)..."
incus config device add "$CONTAINER" devctl proxy \
  listen=tcp:127.0.0.1:4000 connect=tcp:127.0.0.1:4000
success "Port proxy configured."

# ─── Step 12: Wait for devctl HTTP ───────────────────────────────────────────
info "Waiting for devctl HTTP API to respond..."
TIMEOUT=30
ELAPSED=0
while true; do
  if curl -sf http://127.0.0.1:4000/api/settings/resolved >/dev/null 2>&1; then
    success "devctl is responding on http://127.0.0.1:4000."
    break
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    error "Timed out waiting for devctl HTTP API."
    echo ""
    echo "Last 20 lines of devctl journal:"
    incus exec "$CONTAINER" -- journalctl -u devctl -n 20 --no-pager || true
    exit 1
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

# ─── Step 13: Export env vars ─────────────────────────────────────────────────
export DEVCTL_CONTAINER="$CONTAINER"
export DEVCTL_BASE_URL="http://127.0.0.1:4000"

# ─── Step 14: Mode-specific behaviour ────────────────────────────────────────
if [[ "$MODE" == "interactive" ]]; then
  echo ""
  printf '%s─────────────────────────────────────────────────────%s\n' "${BOLD}" "${RESET}"
  printf '%s devctl test environment ready%s\n'                          "${BOLD}" "${RESET}"
  printf ' Container : %s\n'                                             "${CONTAINER}"
  printf ' Dashboard : %s%s%s\n'                                         "${CYAN}" "${DEVCTL_BASE_URL}" "${RESET}"
  echo ""
  printf ' Press Ctrl+C to stop and destroy the container.\n'
  printf '%s─────────────────────────────────────────────────────%s\n' "${BOLD}" "${RESET}"
  echo ""
  # Block until Ctrl+C or EXIT trap fires
  wait
else
  echo ""
  info "Running tests..."
  TEST_EXIT=0
  make test || TEST_EXIT=$?
  # EXIT trap will destroy the container; exit with test result
  exit $TEST_EXIT
fi
