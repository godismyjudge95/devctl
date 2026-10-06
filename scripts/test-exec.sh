#!/usr/bin/env bash
# scripts/test-exec.sh — Run a command in the Incus or OrbStack test machine.
# Usage: test-exec.sh CONTAINER [--cwd DIR] -- command [args...]
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: test-exec.sh CONTAINER [--cwd DIR] -- command [args...]" >&2
  exit 2
fi

NAME="$1"
shift
CWD=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cwd)
      CWD="$2"
      shift 2
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

if command -v orbctl >/dev/null 2>&1; then
  if [[ -n "$CWD" ]]; then
    exec orb run -m "$NAME" -w "$CWD" sudo "$@"
  fi
  exec orb run -m "$NAME" sudo "$@"
fi

echo "test-exec: no Incus container or OrbStack machine named ${NAME}" >&2
exit 1
