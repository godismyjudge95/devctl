#!/usr/bin/env bats
# test_install.bats — Verify that the devctl binary and systemd unit are correctly
# installed inside the container and that the server root directory exists.

load setup

@test "binary exists at /usr/local/bin/devctl" {
  run container_exec test -f /usr/local/bin/devctl
  [ "$status" -eq 0 ]
}

@test "binary is executable" {
  run container_exec test -x /usr/local/bin/devctl
  [ "$status" -eq 0 ]
}

@test "systemd unit file exists at /etc/systemd/system/devctl.service" {
  run container_exec test -f /etc/systemd/system/devctl.service
  [ "$status" -eq 0 ]
}

@test "unit file contains ExecStart=/usr/local/bin/devctl" {
  run container_exec grep -q "ExecStart=/usr/local/bin/devctl" /etc/systemd/system/devctl.service
  [ "$status" -eq 0 ]
}

@test "unit file contains DEVCTL_SITE_USER=testuser" {
  run container_exec grep -q "DEVCTL_SITE_USER=testuser" /etc/systemd/system/devctl.service
  [ "$status" -eq 0 ]
}

@test "unit file contains DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server" {
  run container_exec grep -q "DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server" /etc/systemd/system/devctl.service
  [ "$status" -eq 0 ]
}

@test "server root directory /home/testuser/ddev/sites/server exists" {
  run container_exec test -d /home/testuser/ddev/sites/server
  [ "$status" -eq 0 ]
}
