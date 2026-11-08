package php

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateXdebugMode(t *testing.T) {
	for _, ok := range []string{"debug", "debug,develop", "coverage", "profile,trace"} {
		if err := ValidateXdebugMode(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "off", "debug,", "step", "debug, develop", "debug\nzend_extension=/x.so"} {
		if err := ValidateXdebugMode(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestXdebugStatusFor_AvailableOnlyWhenBuildShipsIt(t *testing.T) {
	root := t.TempDir()
	if st := XdebugStatusFor("8.4", root, nil); st.Available || st.Enabled {
		t.Errorf("no xdebug.so: %+v", st)
	}
	writeXdebugSO(t, root, "8.4")
	if st := XdebugStatusFor("8.4", root, nil); !st.Available || st.Enabled {
		t.Errorf("xdebug.so present, no directives: %+v", st)
	}
}

func TestXdebugStatusFor_EnabledWhenOurExtensionIsLoaded(t *testing.T) {
	root := t.TempDir()
	writeXdebugSO(t, root, "8.4")
	on := XdebugOnDirectives("8.4", root, "debug,develop", nil)
	st := XdebugStatusFor("8.4", root, on)
	if !st.Enabled || st.Mode != "debug,develop" {
		t.Errorf("status = %+v", st)
	}
	other := map[string]string{"zend_extension": "/opt/other.so", "xdebug.mode": "debug"}
	if st := XdebugStatusFor("8.4", root, other); st.Enabled {
		t.Errorf("another zend_extension counted as Xdebug: %+v", st)
	}
}

func TestXdebugOnDirectives_TriggerByDefaultButKeepsUserChoice(t *testing.T) {
	root := t.TempDir()
	kv := XdebugOnDirectives("8.4", root, "debug", nil)
	if kv["zend_extension"] != XdebugPath("8.4", root) || kv["xdebug.mode"] != "debug" || kv["xdebug.start_with_request"] != "trigger" {
		t.Errorf("defaults = %v", kv)
	}
	kv = XdebugOnDirectives("8.4", root, "debug", map[string]string{"xdebug.start_with_request": "yes"})
	if _, set := kv["xdebug.start_with_request"]; set {
		t.Errorf("overwrote the user's start_with_request: %v", kv)
	}
}

func TestXdebugOnAndOff_ManagedBlockRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeXdebugSO(t, root, "8.4")
	ini := writeTempIni(t, "[PHP]\nmemory_limit = 128M\n")
	on := XdebugOnDirectives("8.4", root, "debug", nil)
	if err := WriteManagedBlock(ini, on); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, ini)
	for _, line := range []string{"zend_extension = " + XdebugPath("8.4", root), "xdebug.mode = debug", "xdebug.start_with_request = trigger"} {
		if !containsLine(got, line) {
			t.Errorf("php.ini missing %q:\n%s", line, got)
		}
	}
	for _, k := range XdebugOffKeys {
		delete(on, k)
	}
	if err := WriteManagedBlock(ini, on); err != nil {
		t.Fatal(err)
	}
	got = readFile(t, ini)
	if containsLine(got, "zend_extension = "+XdebugPath("8.4", root)) || !containsLine(got, "xdebug.start_with_request = trigger") {
		t.Errorf("off should drop only the loader lines:\n%s", got)
	}
}

func TestInstallXdebug_DownloadsListedAssetAndRemovesStaleCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/php-binaries-20261108.1/php-8.4-xdebug-macos-aarch64.so" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("xdebug-bytes"))
	}))
	defer srv.Close()
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", srv.URL)

	root := t.TempDir()
	manifest := &ReleaseManifest{
		ReleaseTag: "php-binaries-20261108.1",
		PlatformAssets: map[string]map[string]ReleaseAssets{
			"macos-aarch64": {"8.4": {CLI: "php-8.4-cli-macos-aarch64", FPM: "php-8.4-fpm-macos-aarch64", Xdebug: "php-8.4-xdebug-macos-aarch64.so"}},
			"linux-x86_64":  {"8.4": {CLI: "php-8.4-cli-linux-x86_64", FPM: "php-8.4-fpm-linux-x86_64"}},
		},
	}
	if err := installXdebug(context.Background(), manifest, "8.4", root, "macos-aarch64"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, XdebugPath("8.4", root)); got != "xdebug-bytes" {
		t.Errorf("xdebug.so = %q", got)
	}

	if err := installXdebug(context.Background(), manifest, "8.4", root, "linux-x86_64"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(XdebugPath("8.4", root)); !os.IsNotExist(err) {
		t.Errorf("stale xdebug.so kept for a build without Xdebug: %v", err)
	}
}

func writeXdebugSO(t *testing.T, root, ver string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(XdebugPath(ver, root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(XdebugPath(ver, root), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsLine(content, line string) bool {
	return slices.Contains(strings.Split(content, "\n"), line)
}

func TestReleaseManifest_ReadsXdebugAssetFromWorkflowJSON(t *testing.T) {
	raw := `{"release_tag":"php-binaries-20261108.1","platform_assets":{"macos-aarch64":{"8.4":{"cli":"c","fpm":"f","xdebug":"php-8.4-xdebug-macos-aarch64.so"}},"linux-x86_64":{"8.4":{"cli":"c","fpm":"f"}}}}`
	var m ReleaseManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if a, _ := m.Lookup("8.4", "macos-aarch64"); a.Xdebug != "php-8.4-xdebug-macos-aarch64.so" {
		t.Errorf("macOS assets = %+v", a)
	}
	if a, ok := m.Lookup("8.4", "linux-x86_64"); !ok || a.Xdebug != "" {
		t.Errorf("linux assets = %+v ok=%v", a, ok)
	}
}

func TestInstallXdebug_LinuxGlibcReleaseShipsXdebug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/php-binaries-20261201.1/php-8.4-xdebug-linux-x86_64.so" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("linux-xdebug-bytes"))
	}))
	defer srv.Close()
	t.Setenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE", srv.URL)

	raw := `{"release_tag":"php-binaries-20261201.1","platform_assets":{"linux-x86_64":{"8.4":{"cli":"php-8.4-cli-linux-x86_64","fpm":"php-8.4-fpm-linux-x86_64","xdebug":"php-8.4-xdebug-linux-x86_64.so"}}}}`
	var manifest ReleaseManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := installXdebug(context.Background(), &manifest, "8.4", root, "linux-x86_64"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, XdebugPath("8.4", root)); got != "linux-xdebug-bytes" {
		t.Errorf("xdebug.so = %q", got)
	}
	if st := XdebugStatusFor("8.4", root, nil); !st.Available {
		t.Errorf("Xdebug not available after a Linux install that ships it: %+v", st)
	}
}
