package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseServerRootFromUnit_PlistAndSystemd(t *testing.T) {
	dir := t.TempDir()
	plist := filepath.Join(dir, "ai.devctl.plist")
	if err := os.WriteFile(plist, []byte(`<key>DEVCTL_SERVER_ROOT</key>
		<string>/Users/alice/sites/server</string>`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := parseServerRootFromUnit(plist); got != "/Users/alice/sites/server" {
		t.Errorf("plist = %q", got)
	}

	unit := filepath.Join(dir, "devctl.service")
	if err := os.WriteFile(unit, []byte("Environment=DEVCTL_SERVER_ROOT=/home/alice/sites/server\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := parseServerRootFromUnit(unit); got != "/home/alice/sites/server" {
		t.Errorf("unit = %q", got)
	}
}
