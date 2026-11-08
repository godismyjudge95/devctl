#!/usr/bin/env bash
# scripts/test-exec.sh — Run a command in the Incus or VMPal test machine.
# Usage: test-exec.sh CONTAINER [--cwd DIR] [--stdin] -- command [args...]
# --stdin passes this script's stdin to the command (VMPal copies it in first).
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: test-exec.sh CONTAINER [--cwd DIR] [--stdin] -- command [args...]" >&2
  exit 2
fi

NAME="$1"
shift
CWD=""
STDIN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cwd)
      CWD="$2"
      shift 2
      ;;
    --stdin)
      STDIN=1
      shift
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

if command -v incus >/dev/null 2>&1 && incus list --format csv -c n 2>/dev/null | grep -qx "$NAME"; then
  if [[ -n "$CWD" ]]; then
    exec incus exec "$NAME" --cwd "$CWD" -- "$@"
  fi
  exec incus exec "$NAME" -- "$@"
fi

source "$(dirname "$0")/vmpal-lib.sh"
if vmpal_has_vm "$NAME"; then
  OPTS=(--admin)
  [[ -n "$CWD" ]] && OPTS+=(--cwd "$CWD")
  # vmpal exec has no stdin: copy the input into the VM and redirect it there.
  if [[ "$STDIN" == 1 ]]; then
    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT
    cat > "$TMP"
    GUEST_IN="/tmp/test-exec-stdin-$$"
    vmpal cp --admin "$TMP" "$NAME:$GUEST_IN"
    EXIT=0
    vmpal exec "$NAME" "${OPTS[@]}" -- /bin/sh -c 'in="$1"; shift; "$@" < "$in"; s=$?; rm -f "$in"; exit $s' sh "$GUEST_IN" "$@" || EXIT=$?
    exit $EXIT
  fi
  vmpal exec "$NAME" "${OPTS[@]}" -- "$@"
  exit $?
fi

echo "test-exec: no Incus container or VMPal VM named ${NAME}" >&2
exit 1
