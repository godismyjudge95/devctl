#!/usr/bin/env bash
# scripts/test-cleanup.sh — Stop and destroy a devctl test Incus container or OrbStack VM.
#
# Usage:
#   DEVCTL_CONTAINER=devctl-test-123 scripts/test-cleanup.sh
#   scripts/test-cleanup.sh devctl-test-123
#
# Set KEEP_TEST_CONTAINER=1 to skip cleanup (e.g. iterative test-push runs).
set -euo pipefail

if [[ "${KEEP_TEST_CONTAINER:-}" == "1" ]]; then
  exit 0
fi

CONTAINER="${1:-${DEVCTL_CONTAINER:-}}"
if [[ -z "$CONTAINER" ]]; then
  exit 0
fi

if command -v incus >/dev/null 2>&1 && incus list --format csv -c n 2>/dev/null | grep -qx "$CONTAINER"; then
  echo "Stopping and destroying Incus test container ${CONTAINER}..."
  incus exec "$CONTAINER" -- systemctl stop devctl 2>/dev/null || true
  incus stop "$CONTAINER" --force 2>/dev/null || true
  incus delete --force "$CONTAINER" 2>/dev/null || true
  echo "Container ${CONTAINER} destroyed."
  exit 0
fi

if command -v orbctl >/dev/null 2>&1 && orbctl list -q 2>/dev/null | awk '{print $1}' | grep -qx "$CONTAINER"; then
  echo "Stopping and destroying OrbStack test machine ${CONTAINER}..."
  orb run -m "$CONTAINER" sudo systemctl stop devctl 2>/dev/null || true
  orbctl delete -f "$CONTAINER" 2>/dev/null || true
  echo "Machine ${CONTAINER} destroyed."
  exit 0
fi

echo "No Incus or OrbStack backend to clean up ${CONTAINER}." >&2
exit 0