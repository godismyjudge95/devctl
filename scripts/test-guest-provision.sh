#!/usr/bin/env bash
# scripts/test-guest-provision.sh — Runs as root inside a fresh VMPal test VM.
# Expects /usr/local/bin/devctl to be present. Creates testuser, the PHP 8.4
# stub, the devctl and elevate units, then waits for the dashboard API.
set -euo pipefail

useradd -m testuser || true
mkdir -p /home/testuser/ddev/sites/server
chmod 755 /usr/local/bin/devctl
chown testuser:testuser /usr/local/bin/devctl

PHP_DIR="/home/testuser/ddev/sites/server/php/8.4"
mkdir -p "$PHP_DIR"
printf '#!/bin/sh\n' > "${PHP_DIR}/php-fpm"
chmod 755 "${PHP_DIR}/php-fpm"
cat > "${PHP_DIR}/php.ini" <<'PHPINI'
memory_limit = 128M
max_execution_time = 30
upload_max_filesize = 2M
post_max_size = 8M
PHPINI
chown -R testuser:testuser /home/testuser/ddev

cat > /etc/systemd/system/devctl.service <<'EOF'
[Unit]
Description=devctl — Local PHP Dev Dashboard
After=network.target

[Service]
Type=simple
User=testuser
Group=testuser
NoNewPrivileges=true
ExecStart=/usr/local/bin/devctl daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server
Environment=DEVCTL_TESTING=true

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/devctl-elevate.service <<'EOF'
[Unit]
Description=devctl elevated bind supervisor
After=network.target
Before=devctl.service

[Service]
Type=simple
User=testuser
Group=testuser
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ExecStart=/usr/local/bin/devctl elevate daemon
Restart=on-failure
RestartSec=5s
Environment=HOME=/home/testuser
Environment=DEVCTL_SITE_USER=testuser
Environment=DEVCTL_SERVER_ROOT=/home/testuser/ddev/sites/server
Environment=DEVCTL_ELEVATED=1
Environment=DEVCTL_TESTING=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now devctl-elevate
systemctl enable --now devctl

for _ in $(seq 1 45); do
  if curl -sf http://127.0.0.1:4000/api/settings/resolved >/dev/null 2>&1; then
    echo "devctl is responding."
    exit 0
  fi
  sleep 1
done
echo "Timed out waiting for devctl HTTP API." >&2
journalctl -u devctl -n 30 --no-pager || true
exit 1
