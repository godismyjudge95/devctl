#!/usr/bin/env bash
# scripts/test-env-setup-vmpal.sh — One-time setup of the VMPal Linux VM used
# for tests on macOS (DEVCTL_VMPAL_VM, default "Linux"). Installs the same
# tooling as the Incus base image: curl, jq, git, service runtime libs,
# Node.js 22, bats, @playwright/test with Chromium. This tooling stays in the VM;
# each test run snapshots after it. Idempotent. Suspends the VM when done.
set -euo pipefail
source "$(dirname "$0")/vmpal-lib.sh"

VM="$VMPAL_TEST_VM"
vmpal_has_vm "$VM" || { echo "VMPal VM '${VM}' not found. Set DEVCTL_VMPAL_VM." >&2; exit 1; }

echo "→ Starting ${VM}..."
vmpal start "$VM" --background --wait

admin() { vmpal exec "$VM" --admin --timeout 1h -- "$@"; }

echo "→ Installing base packages..."
admin env DEBIAN_FRONTEND=noninteractive apt-get update -qq
admin env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
  curl jq git build-essential libnuma1 libaio1t64 libreadline-dev

if ! admin /bin/sh -c 'node --version 2>/dev/null | grep -q "^v22\."'; then
  echo "→ Installing Node.js 22..."
  admin /bin/bash -c 'curl -fsSL https://deb.nodesource.com/setup_22.x | bash -'
  admin env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs
fi

echo "→ Installing bats and Playwright with Chromium..."
admin npm install -g bats @playwright/test
admin npx playwright install chromium --with-deps

echo "→ Suspending ${VM}..."
vmpal suspend "$VM"
echo "✓ ${VM} is ready for 'make test-env' / 'make test-run'."
