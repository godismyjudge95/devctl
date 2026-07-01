#!/usr/bin/env bash
# scripts/test-cleanup.sh — Stop and destroy a devctl test Incus container.
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

if ! command -v incus &>/dev/null; then
  echo "Incus is not installed — cannot clean up container ${CONTAINER}." >&2
  exit 1
fi

if ! incus list --format csv -c n 2>/dev/null | grep -qx "$CONTAINER"; then
  exit 0
fi

echo "Stopping and destroying test container ${CONTAINER}..."
incus exec "$CONTAINER" -- systemctl stop devctl 2>/dev/null || true
incus stop "$CONTAINER" --force 2>/dev/null || true
incus delete --force "$CONTAINER" 2>/dev/null || true
echo "Container ${CONTAINER} destroyed."