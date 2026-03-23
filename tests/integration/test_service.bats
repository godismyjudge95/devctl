#!/usr/bin/env bats
# test_service.bats — Verify the devctl systemd service is running correctly and
# has not entered a restart loop or logged any fatal errors.

load setup

@test "systemctl is-active devctl returns exit 0" {
  run container_exec systemctl is-active devctl
  [ "$status" -eq 0 ]
}

@test "systemctl is-enabled devctl returns exit 0" {
  run container_exec systemctl is-enabled devctl
  [ "$status" -eq 0 ]
}

@test "systemctl is-failed devctl returns non-zero (service is NOT failed)" {
  run container_exec systemctl is-failed devctl
  [ "$status" -ne 0 ]
}

@test "service has been active for more than 2 seconds (no restart loop)" {
  # Parse the ActiveEnterTimestamp and compare against the current time.
  active_since=$(container_exec systemctl show devctl --property=ActiveEnterTimestamp \
    | sed 's/ActiveEnterTimestamp=//')
  [ -n "$active_since" ]

  # Convert both timestamps to epoch seconds for arithmetic comparison.
  since_epoch=$(date -d "$active_since" +%s 2>/dev/null || echo 0)
  now_epoch=$(date +%s)
  uptime_secs=$(( now_epoch - since_epoch ))
  [ "$uptime_secs" -gt 2 ]
}

@test "journalctl devctl has no FATAL log lines in last 5 entries" {
  run container_exec bash -c "journalctl -u devctl -n 5 --no-pager | grep -i FATAL"
  # grep exits 1 when no lines match — that is the desired outcome.
  [ "$status" -ne 0 ]
}
