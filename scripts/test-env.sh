#!/usr/bin/env bash
# scripts/test-env.sh — Incus container lifecycle for devctl testing
#
# All tests run INSIDE the container.  The host devctl is never stopped and
# port 4000 on the host is never touched.  Multiple containers can run in
# parallel without any port conflicts.
#
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

# ─── Step 3: Generate container name ─────────────────────────────────────────
CONTAINER="devctl-test-$(date +%s)"

# ─── EXIT trap (set before launch so it always fires) ─────────────────────────
cleanup() {
  echo ""
  echo "Destroying container ${CONTAINER}..."
  incus delete --force "$CONTAINER" 2>/dev/null || true
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

# ─── Step 7a: Stub a PHP 8.4 installation for settings tests ──────────────────
# The PHP settings tests (test_api_php_mutate.bats) require at least one PHP
# version to be "installed" (i.e. php-fpm binary exists under SERVER_ROOT/php/).
# We create a minimal stub: a zero-byte php-fpm binary and a real php.ini
# copied from the embedded devctl template so the read/write round-trip works.
info "Creating PHP 8.4 stub for settings tests..."
PHP_DIR="/home/testuser/ddev/sites/server/php/8.4"
incus exec "$CONTAINER" -- mkdir -p "$PHP_DIR"
# Stub binary — just needs to exist for InstalledVersions() to recognise it.
incus exec "$CONTAINER" -- bash -c "echo '#!/bin/sh' > ${PHP_DIR}/php-fpm && chmod 755 ${PHP_DIR}/php-fpm"
# Write a minimal php.ini with the four tracked settings.
incus exec "$CONTAINER" -- tee "${PHP_DIR}/php.ini" >/dev/null <<'PHPINI'
; devctl stub php.ini — used by PHP settings tests
memory_limit = 128M
max_execution_time = 30
upload_max_filesize = 2M
post_max_size = 8M
PHPINI
incus exec "$CONTAINER" -- chown -R testuser:testuser /home/testuser/ddev
success "PHP 8.4 stub created at ${PHP_DIR}."

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
success "Service unit written."

# ─── Step 9: Enable and start devctl ─────────────────────────────────────────
info "Enabling and starting devctl service..."
incus exec "$CONTAINER" -- systemctl daemon-reload
incus exec "$CONTAINER" -- systemctl enable devctl
incus exec "$CONTAINER" -- systemctl start devctl
success "devctl service started."

# ─── Step 10: Wait for devctl HTTP ───────────────────────────────────────────
info "Waiting for devctl HTTP API to respond..."
TIMEOUT=30
ELAPSED=0
while true; do
  if incus exec "$CONTAINER" -- curl -sf http://127.0.0.1:4000/api/settings/resolved >/dev/null 2>&1; then
    success "devctl is responding on http://127.0.0.1:4000 (inside container)."
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

# ─── Step 10b: Install Caddy (required service) ───────────────────────────────
# Caddy must be installed and running before tests begin. The mutate tests
# assume caddy is pre-installed (it is a required service). Install via the API
# and wait for it to reach running state.
info "Installing Caddy (required service)..."
INSTALL_RESPONSE=$(incus exec "$CONTAINER" -- \
  curl -sf -X POST --max-time 120 --no-buffer \
  http://127.0.0.1:4000/api/services/caddy/install 2>/dev/null || true)
if echo "$INSTALL_RESPONSE" | grep -q "^event: done"; then
  success "Caddy installed."
elif echo "$INSTALL_RESPONSE" | grep -q "^event: error"; then
  error "Caddy install failed:"
  echo "$INSTALL_RESPONSE" | grep "^data:" | tail -5 >&2
  exit 1
else
  # May have already been installed or an unexpected response — check status
  CADDY_INSTALLED=$(incus exec "$CONTAINER" -- \
    sh -c "curl -sf http://127.0.0.1:4000/api/services | jq -r '.[] | select(.id==\"caddy\") | .installed'" 2>/dev/null || echo "unknown")
  if [[ "$CADDY_INSTALLED" == "true" ]]; then
    success "Caddy already installed."
  else
    error "Caddy install did not return a done event (response: ${INSTALL_RESPONSE:0:200})"
    exit 1
  fi
fi

# Wait for Caddy to reach running state
TIMEOUT=30; ELAPSED=0
while true; do
  CADDY_STATUS=$(incus exec "$CONTAINER" -- \
    sh -c "curl -sf http://127.0.0.1:4000/api/services | jq -r '.[] | select(.id==\"caddy\") | .status'" 2>/dev/null || echo "")
  if [[ "$CADDY_STATUS" == "running" ]]; then
    success "Caddy is running."
    break
  fi
  if [[ $ELAPSED -ge $TIMEOUT ]]; then
    error "Timed out waiting for Caddy to reach running state (last: ${CADDY_STATUS})."
    exit 1
  fi
  sleep 1; ELAPSED=$((ELAPSED+1))
done

# ─── Step 10c: Install PHP 8.3 (required by Reverb installer) ────────────────
# The Reverb installer runs `{serverRoot}/bin/php {serverRoot}/bin/composer
# create-project laravel/laravel reverb`.  Those binaries are installed by the
# devctl PHP installer.  Without them the reverb test fails with exit status 127
# (command not found).  Install PHP 8.3 here — the curl shim (step 7c) will
# serve the static binaries from the artifact cache if available, otherwise they
# are downloaded from GitHub.
# NOTE: POST /api/php/versions/{ver}/install returns JSON (not SSE), and blocks
# until the full install completes.  Use a generous --max-time to accommodate
# binary downloads.
info "Installing PHP 8.3 (required by Reverb installer)..."
PHP_INSTALL_STATUS=$(incus exec "$CONTAINER" -- \
  curl -s -o /dev/null -w "%{http_code}" -X POST --max-time 600 \
  http://127.0.0.1:4000/api/php/versions/8.3/install 2>/dev/null || echo "000")
if [[ "$PHP_INSTALL_STATUS" == "200" ]]; then
  success "PHP 8.3 installed."
elif [[ "$PHP_INSTALL_STATUS" == "000" ]]; then
  error "PHP 8.3 install request timed out or failed to connect."
  exit 1
else
  error "PHP 8.3 install returned HTTP ${PHP_INSTALL_STATUS}."
  exit 1
fi

# ─── Step 11: Push test assets into the container ───────────────────────────
# Use tar-pipe for all multi-file transfers: single round-trip instead of one
# Incus API call per file (which is very slow for even small directory trees).

info "Pushing BATS integration tests..."
incus exec "$CONTAINER" -- mkdir -p /tmp/tests
tar -czf - -C tests integration/ | incus exec "$CONTAINER" -- tar -xzf - -C /tmp/tests/
success "BATS tests pushed to /tmp/tests/integration/."

# Go API test binary — compile on host, push binary only (no Go needed in container)
if command -v go &>/dev/null; then
  info "Compiling Go API test binary..."
  go test -c -tags=integration -o devctl.test ./tests/api/ 2>&1
  incus file push devctl.test "$CONTAINER/tmp/devctl.test"
  incus exec "$CONTAINER" -- chmod 755 /tmp/devctl.test
  rm -f devctl.test
  success "Go API test binary pushed to /tmp/devctl.test."
else
  info "Go not found on host — skipping Go API test binary (test-api will be unavailable)."
fi

# Playwright e2e tests — copy test specs and config
info "Pushing Playwright e2e tests..."
tar -czf - -C tests e2e/ | incus exec "$CONTAINER" -- tar -xzf - -C /tmp/tests/
# Write a minimal package.json so npx playwright resolves correctly and push config
printf '{\n  "name": "devctl-tests",\n  "private": true\n}\n' \
  | incus exec "$CONTAINER" -- tee /tmp/package.json >/dev/null
incus file push playwright.config.ts "$CONTAINER/tmp/playwright.config.ts"
success "Playwright e2e tests pushed to /tmp/tests/e2e/."

# ─── Step 12: Export env vars ─────────────────────────────────────────────────
export DEVCTL_CONTAINER="$CONTAINER"

# ─── Step 13: Mode-specific behaviour ────────────────────────────────────────
if [[ "$MODE" == "interactive" ]]; then
  echo ""
  printf '%s─────────────────────────────────────────────────────%s\n' "${BOLD}" "${RESET}"
  printf '%s devctl test environment ready%s\n'                          "${BOLD}" "${RESET}"
  printf ' Container : %s\n'                                             "${CONTAINER}"
  printf ' Tests run : %sinside the container%s (host devctl untouched)\n' "${CYAN}" "${RESET}"
  echo ""
  printf ' To run tests:\n'
  printf '   DEVCTL_CONTAINER=%s make test-bats\n'                       "${CONTAINER}"
  printf '   DEVCTL_CONTAINER=%s make test-api\n'                        "${CONTAINER}"
  printf '   DEVCTL_CONTAINER=%s make test-e2e\n'                        "${CONTAINER}"
  printf '   DEVCTL_CONTAINER=%s make test\n'                            "${CONTAINER}"
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
  DEVCTL_CONTAINER="$CONTAINER" make test || TEST_EXIT=$?
  # EXIT trap will destroy the container; exit with test result
  exit $TEST_EXIT
fi
