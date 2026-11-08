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

source "$(dirname "$0")/vmpal-lib.sh"
if vmpal_has_vm "$NAME"; then
  vmpal cp --admin "$SRC" "$NAME:$DEST"
  exit 0
fi

echo "test-push: no Incus container or VMPal VM named ${NAME}" >&2
exit 1
