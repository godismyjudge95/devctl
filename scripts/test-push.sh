#!/usr/bin/env bash
# scripts/test-push.sh — Copy a local file into the test machine.
# Usage: test-push.sh CONTAINER LOCAL_PATH DEST_PATH
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: test-push.sh CONTAINER LOCAL_PATH DEST_PATH" >&2
  exit 2
fi

NAME="$1"
SRC="$2"
DEST="$3"

if command -v incus >/dev/null 2>&1 && incus list --format csv -c n 2>/dev/null | grep -qx "$NAME"; then
  incus file push "$SRC" "$NAME$DEST"
  exit 0
fi

if command -v orbctl >/dev/null 2>&1; then
  # OrbStack machines see the Mac filesystem at the same path.
  ABS="$(cd "$(dirname "$SRC")" && pwd)/$(basename "$SRC")"
  orb run -m "$NAME" sudo cp "$ABS" "$DEST"
  exit 0
fi

echo "test-push: no Incus container or OrbStack machine named ${NAME}" >&2
exit 1
