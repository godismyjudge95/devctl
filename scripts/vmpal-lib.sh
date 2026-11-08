# scripts/vmpal-lib.sh — sourced by the test scripts for the VMPal backend (macOS).
#
# Tests run in an existing VMPal Linux VM (DEVCTL_VMPAL_VM, default "Linux").
# test-env-vmpal.sh snapshots it as $VMPAL_TEST_SNAPSHOT before devctl is
# installed; test-cleanup.sh reverts to that snapshot, deletes it, and
# suspends the VM, so the VM ends exactly as it was.

VMPAL_APP_BIN="/Applications/VMPal.app/Contents/Helpers/VMPalMachine.app/Contents/MacOS/VMPalMachine"
VMPAL_TEST_VM="${DEVCTL_VMPAL_VM:-Linux}"
VMPAL_TEST_SNAPSHOT="devctl-test-env"

vmpal() {
  if type -P vmpal >/dev/null 2>&1; then
    command vmpal "$@"
  else
    "$VMPAL_APP_BIN" --cli "$@"
  fi
}

vmpal_available() {
  type -P vmpal >/dev/null 2>&1 || [[ -x "$VMPAL_APP_BIN" ]]
}

vmpal_has_vm() {
  vmpal_available && vmpal info "$1" >/dev/null 2>&1
}

vmpal_has_test_snapshot() {
  vmpal snapshots "$1" --json 2>/dev/null | jq -e --arg n "$VMPAL_TEST_SNAPSHOT" '.. | objects | select(.name? == $n)' >/dev/null
}
