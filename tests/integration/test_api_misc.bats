#!/usr/bin/env bats
# test_api_misc.bats — Miscellaneous endpoint coverage:
#   GET /               — SPA index
#   GET /api/tls/cert   — Caddy root CA (502 when Caddy not installed; accept both)
#   GET /api/dns/detect-ip
#   GET /api/dns/setup
#   GET /api/nonexistent — must return 404, not 500

load setup

@test "GET / returns HTTP 200 (SPA index)" {
  status=$(api_status /)
  [ "$status" -eq 200 ]
}

@test "GET / response body contains html markup" {
  body=$(api_get /)
  [[ "$body" == *"<html"* ]] || [[ "$body" == *"<!DOCTYPE"* ]] || [[ "$body" == *"<!doctype"* ]]
}

@test "GET /api/tls/cert returns 200 or 502 (502 when Caddy is not installed)" {
  # 200 = cert retrieved; 502 = caddy not available — both are valid in test env.
  status=$(api_status /api/tls/cert)
  [ "$status" -eq 200 ] || [ "$status" -eq 502 ]
}

@test "GET /api/dns/detect-ip returns HTTP 200" {
  status=$(api_status /api/dns/detect-ip)
  [ "$status" -eq 200 ]
}

@test "GET /api/dns/detect-ip response contains ip field" {
  result=$(api_get /api/dns/detect-ip)
  assert_json_field "$result" "ip"
}

@test "GET /api/dns/setup returns HTTP 200" {
  status=$(api_status /api/dns/setup)
  [ "$status" -eq 200 ]
}

@test "GET /api/dns/setup response contains configured field" {
  result=$(api_get /api/dns/setup)
  assert_json_field "$result" "configured"
}

@test "GET /api/nonexistent returns HTTP 404 (not 500)" {
  status=$(api_status /api/nonexistent)
  [ "$status" -eq 404 ]
}
