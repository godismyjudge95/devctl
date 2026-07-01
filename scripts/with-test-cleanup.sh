#!/usr/bin/env bash
# scripts/with-test-cleanup.sh — Run a command, then destroy the test container.
#
# Respects KEEP_TEST_CONTAINER=1 to leave the container running.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ $# -lt 1 ]]; then
  echo "usage: with-test-cleanup.sh <command> [args...]" >&2
  exit 1
fi

EXIT=0
"$@" || EXIT=$?

bash "$SCRIPT_DIR/test-cleanup.sh" || true
exit $EXIT