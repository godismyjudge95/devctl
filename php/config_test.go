package php

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// currentUser returns the username of whoever is running the test process.
// Used so WriteConfigs tests can pass a siteUser that actually exists.
func currentUser(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	return u.Username
}

// setupFakeServerRoot creates a minimal fake server root for WriteConfigs tests.
// It creates the PHP version directory with a fake php-fpm binary so
// InstalledVersions recognises it, and returns the serverRoot path.
func setupFakeServerRoot(t *testing.T, ver string) string {
	t.Helper()
	serverRoot := t.TempDir()

	phpDir := PHPDir(ver, serverRoot)
	if err := os.MkdirAll(phpDir, 0755); err != nil {
		t.Fatalf("create php dir: %v", err)
	}

	// Create a fake php-fpm binary so the version is considered installed.
	fpmBin := filepath.Join(phpDir, "php-fpm")
	if err := os.WriteFile(fpmBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("create fake php-fpm: %v", err)
	}

	// Ensure the logs directory exists (WriteConfigs doesn't create it).
	logsDir := filepath.Join(serverRoot, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}

	return serverRoot
}

// readFPMConf calls WriteConfigs and returns the generated php-fpm.conf content.
// It uses the current OS user as siteUser so chown calls succeed in tests.
func readFPMConf(t *testing.T, ver, serverRoot string) string {
	t.Helper()
	siteUser := currentUser(t)
	if err := WriteConfigs(ver, serverRoot, siteUser); err != nil {
		t.Fatalf("WriteConfigs: %v", err)
	}
	data, err := os.ReadFile(FPMConfigPath(ver, serverRoot))
	if err != nil {
		t.Fatalf("read fpm conf: %v", err)
	}
	return string(data)
}

// TestWriteConfigs_FPMConf_ErrorLogIsPoolLog verifies that the [www] pool
// error_log directive points to the central logs directory with the correct
// name (php-fpm-<ver>.log), not to a path inside the PHP version directory.
func TestWriteConfigs_FPMConf_ErrorLogIsPoolLog(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	conf := readFPMConf(t, ver, serverRoot)

	expectedPoolLog := filepath.Join(serverRoot, "logs", "php-fpm-8.4.log")
	if !strings.Contains(conf, "php_value[error_log] = "+expectedPoolLog) {
		t.Errorf("php-fpm.conf missing expected pool error_log directive\nwant: php_value[error_log] = %s\ngot conf:\n%s", expectedPoolLog, conf)
	}
}

// TestWriteConfigs_FPMConf_HTMLErrorsOff verifies that php_value[html_errors]
// is set to Off in the [www] pool. Without this, PHP defaults html_errors=On
// in web context, wrapping log entries in HTML tags.
func TestWriteConfigs_FPMConf_HTMLErrorsOff(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	conf := readFPMConf(t, ver, serverRoot)

	if !strings.Contains(conf, "php_value[html_errors] = Off") {
		t.Errorf("php-fpm.conf missing php_value[html_errors] = Off\ngot conf:\n%s", conf)
	}
}

// TestWriteConfigs_FPMConf_NoHTMLInErrorLog verifies that the error log path
// directive uses php_value (not php_admin_value) — the user's php.ini is the
// source of truth for all settings except auto_prepend_file.
func TestWriteConfigs_FPMConf_ErrorLogUsesPHPValue(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	conf := readFPMConf(t, ver, serverRoot)

	if strings.Contains(conf, "php_admin_value[error_log]") {
		t.Errorf("php-fpm.conf must not use php_admin_value[error_log]; use php_value instead\ngot conf:\n%s", conf)
	}
	if strings.Contains(conf, "php_admin_flag[log_errors]") {
		t.Errorf("php-fpm.conf must not use php_admin_flag[log_errors]; log_errors is managed by php.ini\ngot conf:\n%s", conf)
	}
}

// TestWriteConfigs_FPMConf_GlobalErrorLog verifies the [global] error_log
// directive points to the -global log file (FPM startup/shutdown messages).
func TestWriteConfigs_FPMConf_GlobalErrorLog(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	conf := readFPMConf(t, ver, serverRoot)

	expectedGlobalLog := filepath.Join(serverRoot, "logs", "php-fpm-8.4-global.log")
	if !strings.Contains(conf, "error_log = "+expectedGlobalLog) {
		t.Errorf("php-fpm.conf [global] missing expected error_log\nwant: error_log = %s\ngot conf:\n%s", expectedGlobalLog, conf)
	}
}

// TestWriteConfigs_FPMConf_PrependUsesAdminValue verifies that auto_prepend_file
// uses php_admin_value so user code cannot disable the dump interceptor.
func TestWriteConfigs_FPMConf_PrependUsesAdminValue(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	conf := readFPMConf(t, ver, serverRoot)

	if !strings.Contains(conf, "php_admin_value[auto_prepend_file]") {
		t.Errorf("php-fpm.conf auto_prepend_file must use php_admin_value\ngot conf:\n%s", conf)
	}
}

// ownerUID returns the uid of the file/dir at path, or -1 on error.
func ownerUID(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1
	}
	return int(stat.Uid)
}

// TestWriteConfigs_PoolLogOwnedBySiteUser verifies that WriteConfigs creates
// the pool log file and sets its owner to siteUser. FPM workers run as
// siteUser and must be able to open the file for writing; if it is root-owned
// errors are silently lost.
func TestWriteConfigs_PoolLogOwnedBySiteUser(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	siteUser := currentUser(t)

	if err := WriteConfigs(ver, serverRoot, siteUser); err != nil {
		t.Fatalf("WriteConfigs: %v", err)
	}

	u, err := user.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	var expectedUID int
	fmt.Sscan(u.Uid, &expectedUID)

	poolLog := filepath.Join(serverRoot, "logs", "php-fpm-8.4.log")

	if _, err := os.Stat(poolLog); os.IsNotExist(err) {
		t.Fatalf("pool log file was not created: %s", poolLog)
	}

	if gotUID := ownerUID(poolLog); gotUID != expectedUID {
		t.Errorf("pool log %s: want uid %d (siteUser), got uid %d — FPM workers cannot write to it",
			poolLog, expectedUID, gotUID)
	}
}

// TestWriteConfigs_PHPDirOwnedBySiteUser verifies that the PHP version
// directory and its config files are owned by siteUser after WriteConfigs.
// devctl runs as root so these end up root-owned without an explicit chown.
func TestWriteConfigs_PHPDirOwnedBySiteUser(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	siteUser := currentUser(t)

	if err := WriteConfigs(ver, serverRoot, siteUser); err != nil {
		t.Fatalf("WriteConfigs: %v", err)
	}

	u, err := user.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	var expectedUID int
	fmt.Sscan(u.Uid, &expectedUID)

	paths := []string{
		PHPDir(ver, serverRoot),
		FPMConfigPath(ver, serverRoot),
		PHPIniPath(ver, serverRoot),
	}
	for _, p := range paths {
		if gotUID := ownerUID(p); gotUID != expectedUID {
			t.Errorf("%s: want uid %d (siteUser), got uid %d", p, expectedUID, gotUID)
		}
	}
}

// TestWriteConfigs_LogsDirOwnedBySiteUser verifies that the central logs
// directory is owned by siteUser after WriteConfigs.
func TestWriteConfigs_LogsDirOwnedBySiteUser(t *testing.T) {
	ver := "8.4"
	serverRoot := setupFakeServerRoot(t, ver)
	siteUser := currentUser(t)

	if err := WriteConfigs(ver, serverRoot, siteUser); err != nil {
		t.Fatalf("WriteConfigs: %v", err)
	}

	u, err := user.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	var expectedUID int
	fmt.Sscan(u.Uid, &expectedUID)

	logsDir := filepath.Join(serverRoot, "logs")
	if gotUID := ownerUID(logsDir); gotUID != expectedUID {
		t.Errorf("logs dir %s: want uid %d (siteUser), got uid %d", logsDir, expectedUID, gotUID)
	}
}
