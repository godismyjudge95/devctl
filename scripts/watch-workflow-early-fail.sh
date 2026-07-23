#!/usr/bin/env bash
# Poll a GitHub Actions run and abort early on the first failed job.
#
# Usage:
#   scripts/watch-workflow-early-fail.sh <run-id> [poll-seconds] [cancel-on-fail=1]
#
# Behavior:
#   - Polls job status every POLL seconds (default 20)
#   - On first job with conclusion=failure:
#       * prints the failed job name(s)
#       * dumps the last ~80 lines of that job's log
#       * cancels the whole run (so siblings stop burning minutes)
#       * exits 1
#   - On full success: exits 0
#   - On cancel/skip-only completion: exits 2
#
# Timing tip: after a known-fast failure mode (e.g. PHP 7.0 patch fail ~2m),
# use a short poll (15–20s). Don't wait for gh run watch to finish the matrix.

set -euo pipefail

RUN_ID="${1:?usage: $0 <run-id> [poll-seconds] [cancel-on-fail=1]}"
POLL="${2:-20}"
CANCEL_ON_FAIL="${3:-1}"

if ! command -v gh >/dev/null; then
  echo "gh CLI required" >&2
  exit 127
fi

echo "watching run $RUN_ID (poll=${POLL}s cancel_on_fail=${CANCEL_ON_FAIL})"

dump_job_log() {
  local job_id="$1"
  local name="$2"
  echo
  echo "======== failed job: $name (id=$job_id) ========"
  # API returns a zip-ish text log; strip NULs and take the failure tail.
  if gh api "repos/{owner}/{repo}/actions/jobs/${job_id}/logs" 2>/dev/null \
    | tr -d '\000' \
    | rg -i '✗|error:|ERROR|Fatal|Exception|not support|only available|Cannot|failed to|##\[error\]' \
    | tail -40; then
    :
  else
    gh api "repos/{owner}/{repo}/actions/jobs/${job_id}/logs" 2>/dev/null \
      | tr -d '\000' \
      | tail -c 8000 \
      | tail -60 || true
  fi
  echo "======== end log: $name ========"
}

while true; do
  json=$(gh run view "$RUN_ID" --json status,conclusion,jobs \
    --jq '{status, conclusion, jobs: [.jobs[] | {name, status, conclusion, databaseId, startedAt, completedAt}]}')

  status=$(echo "$json" | jq -r .status)
  conclusion=$(echo "$json" | jq -r .conclusion)

  # Snapshot
  echo "$json" | jq -r '
    .jobs[]
    | "\(.status)/\(.conclusion // "-")\t\(.name)"
  ' | sed "s/^/  /"

  # Any hard failures?
  failed=$(echo "$json" | jq -c '[.jobs[] | select(.conclusion == "failure")]')
  fail_count=$(echo "$failed" | jq 'length')

  if [[ "$fail_count" -gt 0 ]]; then
    echo
    echo "EARLY FAIL: $fail_count job(s) failed"
    echo "$failed" | jq -r '.[] | "  - \(.name) (id=\(.databaseId))"'

    # Dump log for the first failure (enough to start fixing)
    first_id=$(echo "$failed" | jq -r '.[0].databaseId')
    first_name=$(echo "$failed" | jq -r '.[0].name')
    dump_job_log "$first_id" "$first_name"

    if [[ "$CANCEL_ON_FAIL" == "1" && "$status" == "in_progress" ]]; then
      echo
      echo "cancelling run $RUN_ID so remaining matrix jobs stop..."
      gh run cancel "$RUN_ID" || true
    fi
    exit 1
  fi

  if [[ "$status" == "completed" ]]; then
    echo
    echo "run completed: conclusion=$conclusion"
    case "$conclusion" in
      success) exit 0 ;;
      cancelled|skipped) exit 2 ;;
      *) exit 1 ;;
    esac
  fi

  sleep "$POLL"
done
