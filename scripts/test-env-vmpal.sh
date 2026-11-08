#!/usr/bin/env bash
# scripts/test-env-vmpal.sh — VMPal test VM lifecycle for Linux integration tests
# on macOS. Starts the existing VM (DEVCTL_VMPAL_VM, default "Linux"), snapshots
# it, installs a linux/arm64 devctl, and on exit reverts to the snapshot and
# suspends the VM. The host Darwin daemon is never used.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/vmpal-lib.sh"

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

VM="$VMPAL_TEST_VM"

if ! vmpal_has_vm "$VM"; then
  error "VMPal VM '${VM}' not found. Set DEVCTL_VMPAL_VM, then run 'make test-env-setup' once."
  exit 1
fi
if vmpal_has_test_snapshot "$VM"; then
  error "Snapshot '${VMPAL_TEST_SNAPSHOT}' already exists on ${VM}: another test run is active, or one crashed."
  error "Run 'DEVCTL_CONTAINER=${VM} make test-cleanup' to revert and remove it."
  exit 1
fi

BIN="./devctl"
if [[ ! -f "$BIN" ]] || file "$BIN" | grep -q 'Mach-O'; then
  info "Building linux/arm64 binary for the VMPal test VM..."
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION:-dev}" -o /tmp/devctl.linux .
  BIN="/tmp/devctl.linux"
fi

info "Starting ${VM}..."
vmpal start "$VM" --background --wait >/dev/null

info "Taking snapshot '${VMPAL_TEST_SNAPSHOT}'..."
vmpal snapshot "$VM" "$VMPAL_TEST_SNAPSHOT" >/dev/null

cleanup() {
  echo ""
  bash "$SCRIPT_DIR/test-cleanup.sh" "$VM"
}
trap cleanup EXIT

# After a resume, the guest's VMPal root service can need a few seconds
# before administrator commands work.
for _ in $(seq 1 60); do
  vmpal exec "$VM" --admin -- true >/dev/null 2>&1 && break
  sleep 2
done

info "Installing devctl and starting the units..."
vmpal cp --admin "$BIN" "$VM:/usr/local/bin/devctl"
vmpal cp --admin "$SCRIPT_DIR/test-guest-provision.sh" "$VM:/tmp/test-guest-provision.sh"
vmpal exec "$VM" --admin -- bash /tmp/test-guest-provision.sh
success "devctl is responding inside ${VM}."

export DEVCTL_CONTAINER="$VM"
echo "export DEVCTL_CONTAINER=${VM}"

if [[ "$MODE" == "run-tests" ]]; then
  info "Running API tests..."
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=integration -o /tmp/devctl.api.test ./tests/api/
  vmpal cp --admin /tmp/devctl.api.test "$VM:/tmp/devctl.test"
  vmpal exec "$VM" --admin -- chmod 755 /tmp/devctl.test
  vmpal exec "$VM" --admin -- env DEVCTL_BASE_URL=http://127.0.0.1:4000 DEVCTL_SITE_USER=testuser /tmp/devctl.test -test.v
fi

if [[ "$MODE" == "interactive" ]]; then
  info "VM stays in test state until Ctrl+C (then it is reverted and suspended)."
  while true; do sleep 3600; done
fi
