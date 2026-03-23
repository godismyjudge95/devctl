#!/usr/bin/env bash
# scripts/test-env-setup.sh — One-time setup for the devctl test environment:
#   1. Build the devctl-ubuntu-base Incus image with all test tooling baked in.
#   2. Create the devctl-test-artifacts persistent storage volume (for download caching).
#
# Baked into the image:
#   - curl, jq (API helpers)
#   - Node.js 22 (for bats + playwright)
#   - bats-core (BATS integration tests run inside the container)
#   - @playwright/test + Chromium (e2e tests run fully inside the container)
#
# Run once before your first test run, and again when you want to refresh the
# base image or add new tooling.  Idempotent — safe to re-run.
set -euo pipefail

if [ -t 1 ] && command -v tput &>/dev/null && tput colors &>/dev/null && [ "$(tput colors)" -ge 8 ]; then
  GREEN="$(tput setaf 2)"; CYAN="$(tput setaf 6)"; RED="$(tput setaf 1)"; RESET="$(tput sgr0)"
else
  GREEN="" CYAN="" RED="" RESET=""
fi
info()    { printf '%s→ %s%s\n' "${CYAN}"  "$*" "${RESET}"; }
success() { printf '%s✓ %s%s\n' "${GREEN}" "$*" "${RESET}"; }
error()   { printf '%s✗ %s%s\n' "${RED}"   "$*" "${RESET}" >&2; }

BUILDER="devctl-base-builder-$(date +%s)"

cleanup() {
  incus delete --force "$BUILDER" 2>/dev/null || true
}
trap cleanup EXIT

info "Launching builder container from images:ubuntu/24.04..."
incus launch images:ubuntu/24.04 "$BUILDER"

info "Waiting for systemd..."
ELAPSED=0
while true; do
  STATE="$(incus exec "$BUILDER" -- systemctl is-system-running 2>/dev/null || true)"
  [[ "$STATE" == "running" || "$STATE" == "degraded" ]] && break
  [[ $ELAPSED -ge 30 ]] && { error "Timed out waiting for systemd."; exit 1; }
  sleep 1; ELAPSED=$((ELAPSED+1))
done
success "Systemd ready."

info "Installing base prerequisites (curl, jq, runtime libs for devctl services)..."
incus exec "$BUILDER" -- apt-get update -qq
incus exec "$BUILDER" -- apt-get install -y -qq \
  curl jq \
  libnuma1 \
  libaio1t64 \
  libreadline-dev
success "Base prerequisites installed."

# ─── Node.js 22 via NodeSource ────────────────────────────────────────────────
info "Installing Node.js 22..."
incus exec "$BUILDER" -- bash -c "curl -fsSL https://deb.nodesource.com/setup_22.x | bash -"
incus exec "$BUILDER" -- apt-get install -y -qq nodejs
NODE_VER="$(incus exec "$BUILDER" -- node --version 2>/dev/null || echo 'unknown')"
success "Node.js installed: ${NODE_VER}"

# ─── bats-core (global npm install) ───────────────────────────────────────────
info "Installing bats-core..."
incus exec "$BUILDER" -- npm install -g bats
BATS_VER="$(incus exec "$BUILDER" -- bats --version 2>/dev/null || echo 'unknown')"
success "bats installed: ${BATS_VER}"

# ─── Playwright CLI + Chromium ────────────────────────────────────────────────
# We install @playwright/test globally so 'npx playwright' is available without
# a local node_modules.  Chromium and its system dependencies are installed once
# here so every test container starts with a ready-to-use headless browser.
info "Installing @playwright/test (this may take a minute)..."
incus exec "$BUILDER" -- npm install -g @playwright/test
PW_VER="$(incus exec "$BUILDER" -- npx playwright --version 2>/dev/null || echo 'unknown')"
success "Playwright installed: ${PW_VER}"

info "Installing Playwright Chromium + system dependencies (this may take several minutes)..."
incus exec "$BUILDER" -- npx playwright install chromium --with-deps
success "Playwright Chromium installed."

# ─── Clean up package manager caches ─────────────────────────────────────────
info "Cleaning apt and npm caches..."
incus exec "$BUILDER" -- apt-get clean
incus exec "$BUILDER" -- npm cache clean --force 2>/dev/null || true
success "Caches cleaned."

info "Publishing image as devctl-ubuntu-base..."
# Remove the old alias if it exists, stop the container, publish and tag.
incus image delete devctl-ubuntu-base 2>/dev/null || true
incus stop "$BUILDER"
incus publish "$BUILDER" --alias devctl-ubuntu-base
success "Image published as devctl-ubuntu-base."

# ─── Step 2: Create the artifact cache storage volume ─────────────────────────
POOL="default"
VOLUME="devctl-test-artifacts"

if incus storage volume show "$POOL" "$VOLUME" &>/dev/null; then
  success "Artifact cache volume '${VOLUME}' already exists — skipping creation."
else
  info "Creating persistent artifact cache volume '${VOLUME}'..."
  incus storage volume create "$POOL" "$VOLUME"
  success "Volume '${VOLUME}' created in pool '${POOL}'."
fi

echo ""
success "Setup complete."
echo "  • Run 'sudo make test-artifacts-download' to pre-download service binaries."
echo "  • Run 'make build && make test-env' to launch an interactive test container."
echo "  • Run 'make build && make test-run' to run the full test suite."
