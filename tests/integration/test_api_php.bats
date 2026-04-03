#!/usr/bin/env bats
# test_api_php.bats — Verify GET /api/php/versions and /api/php/settings.

load setup

@test "GET /api/php/versions returns HTTP 200" {
  status=$(api_status /api/php/versions)
  [ "$status" -eq 200 ]
}

@test "GET /api/php/versions response is a valid JSON array" {
  result=$(api_get /api/php/versions)
  echo "$result" | jq -e '. | type == "array"' > /dev/null
}

@test "GET /api/php/settings returns HTTP 200" {
  status=$(api_status /api/php/settings)
  [ "$status" -eq 200 ]
}

@test "PHP settings response contains memory_limit key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "memory_limit"
}

@test "PHP settings response contains max_execution_time key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "max_execution_time"
}

@test "PHP settings response contains upload_max_filesize key" {
  result=$(api_get /api/php/settings)
  assert_json_field "$result" "upload_max_filesize"
}

# ─── PHP-FPM pool config directives ───────────────────────────────────────────
# Only auto_prepend_file uses php_admin_value (to prevent user code from
# disabling the dump interceptor). All other per-pool php_* directives use
# php_value so users can override them if needed.

@test "php-fpm.conf: only auto_prepend_file uses php_admin_value" {
  fpm_conf="${SERVER_ROOT}/php/8.4/php-fpm.conf"
  # php_admin_value[auto_prepend_file] must be present
  container_exec grep -q "php_admin_value\[auto_prepend_file\]" "$fpm_conf"
  # No other php_admin_* directives should appear
  count=$(container_exec grep -c "php_admin_" "$fpm_conf" || true)
  [ "$count" -eq 1 ]
}

@test "php-fpm.conf: error_log uses php_value not php_admin_value" {
  fpm_conf="${SERVER_ROOT}/php/8.4/php-fpm.conf"
  container_exec grep -q "^php_value\[error_log\]" "$fpm_conf"
  run container_exec grep -q "php_admin_value\[error_log\]" "$fpm_conf"
  [ "$status" -ne 0 ]
}

# ─── PHP error log formatting ──────────────────────────────────────────────────
# html_errors must be Off so error logs contain plain text, not HTML markup.

@test "php.ini: fresh install includes html_errors = Off for plain-text error logs" {
  php_ini="${SERVER_ROOT}/php/8.4/php.ini"
  # Remove the existing php.ini so devctl writes a fresh one (with the full
  # php.ini-development template + devctl overrides) on the next startup.
  container_exec rm -f "$php_ini"
  container_exec systemctl restart devctl
  sleep 2
  container_exec grep -q "^html_errors = Off" "$php_ini"
}
