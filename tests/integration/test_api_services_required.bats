#!/usr/bin/env bats
# test_api_services_required.bats — Verify that required services (caddy, dns)
# cannot be stopped or purged via the API (should return 403 Forbidden).

load setup

@test "caddy stop is blocked with 403" {
  status=$(api_post_status /api/services/caddy/stop "")
  [ "$status" -eq 403 ]
}

@test "caddy purge is blocked with 403" {
  status=$(api_delete /api/services/caddy)
  [ "$status" -eq 403 ]
}

@test "dns stop is blocked with 403" {
  status=$(api_post_status /api/services/dns/stop "")
  [ "$status" -eq 403 ]
}

@test "dns purge is blocked with 403" {
  status=$(api_delete /api/services/dns)
  [ "$status" -eq 403 ]
}

@test "caddy is still running after stop attempt" {
  # The stop call was rejected, so caddy must still be running.
  poll_service_status caddy running 10
}
