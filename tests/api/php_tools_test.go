//go:build integration

package apitest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/php"
)

// TestInstallLaravelCLI_CallsComposerGlobalRequire verifies that
// InstallLaravelCLI invokes `composer global require laravel/installer` as the
// site user and that the expected binary ends up at the standard Composer
// global bin path.
//
// A fake `composer` stub is placed at /usr/local/bin/composer (sudo's
// secure_path) for the duration of the test. The stub records its arguments
// to a world-writable log file inside siteHome and creates the expected
// laravel binary so the function reports success.
func TestInstallLaravelCLI_CallsComposerGlobalRequire(t *testing.T) {
	siteUser, siteHome := requireSiteUserT(t)
	laravelBin := filepath.Join(siteHome, ".config", "composer", "vendor", "bin", "laravel")

	logFile := filepath.Join(siteHome, "composer-laravel-test.log")
	installComposerStub(t, logFile, laravelBin)
	t.Cleanup(func() {
		os.Remove(logFile)
		os.Remove(laravelBin)
	})

	if err := php.InstallLaravelCLI(context.Background(), siteUser, siteHome); err != nil {
		t.Fatalf("InstallLaravelCLI: %v", err)
	}

	log := mustReadFile(t, logFile)
	if !strings.Contains(log, "global require laravel/installer") {
		t.Errorf("composer stub not called with 'global require laravel/installer'\ngot: %s", log)
	}
	assertExecutable(t, laravelBin)
}

// TestInstallStatamicCLI_CallsComposerGlobalRequire verifies that
// InstallStatamicCLI invokes `composer global require statamic/cli` as the
// site user and that the expected binary exists at the standard Composer
// global bin path.
func TestInstallStatamicCLI_CallsComposerGlobalRequire(t *testing.T) {
	siteUser, siteHome := requireSiteUserT(t)
	statamicBin := filepath.Join(siteHome, ".config", "composer", "vendor", "bin", "statamic")

	logFile := filepath.Join(siteHome, "composer-statamic-test.log")
	installComposerStub(t, logFile, statamicBin)
	t.Cleanup(func() {
		os.Remove(logFile)
		os.Remove(statamicBin)
	})

	if err := php.InstallStatamicCLI(context.Background(), siteUser, siteHome); err != nil {
		t.Fatalf("InstallStatamicCLI: %v", err)
	}

	log := mustReadFile(t, logFile)
	if !strings.Contains(log, "global require statamic/cli") {
		t.Errorf("composer stub not called with 'global require statamic/cli'\ngot: %s", log)
	}
	assertExecutable(t, statamicBin)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// requireSiteUserT returns DEVCTL_SITE_USER and its home directory.
// The test is skipped if the variable is not set.
func requireSiteUserT(t *testing.T) (string, string) {
	t.Helper()
	user := os.Getenv("DEVCTL_SITE_USER")
	if user == "" {
		t.Skip("DEVCTL_SITE_USER not set — skipping (requires root + site user env)")
	}
	home := siteUserHome(t, user)
	return user, home
}

// siteUserHome looks up the home directory of the given OS user via getent.
func siteUserHome(t *testing.T, username string) string {
	t.Helper()
	out, err := exec.Command("getent", "passwd", username).Output()
	if err != nil {
		t.Fatalf("getent passwd %s: %v", username, err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), ":", 7)
	if len(parts) < 7 {
		t.Fatalf("unexpected passwd output for %q: %q", username, string(out))
	}
	return parts[5]
}

// installComposerStub replaces /usr/local/bin/composer with a spy stub for the
// duration of the test. The stub:
//   - appends its arguments to logFile (world-writable, created by this func)
//   - creates binPath (and its parent directories) as an executable file
//   - exits 0
//
// Any pre-existing /usr/local/bin/composer is renamed to .bak and restored on
// cleanup. The test must be running as root.
func installComposerStub(t *testing.T, logFile, binPath string) {
	t.Helper()

	// Create a world-writable log file so the stub (running as siteUser) can
	// append to it. os.OpenFile respects the process umask, so we must
	// explicitly chmod 0666 after creation to guarantee world-writability.
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("create log file %s: %v", logFile, err)
	}
	f.Close()
	if err := os.Chmod(logFile, 0666); err != nil {
		t.Fatalf("chmod log file %s: %v", logFile, err)
	}

	script := "#!/bin/sh\n" +
		"echo \"$*\" >> " + logFile + "\n" +
		"mkdir -p " + filepath.Dir(binPath) + "\n" +
		"touch " + binPath + " && chmod 755 " + binPath + "\n" +
		"exit 0\n"

	const systemComposer = "/usr/local/bin/composer"
	backup := systemComposer + ".test-bak"

	// Rename any existing composer out of the way.
	if _, lErr := os.Lstat(systemComposer); lErr == nil {
		if rErr := os.Rename(systemComposer, backup); rErr != nil {
			t.Fatalf("backup composer: %v", rErr)
		}
		t.Cleanup(func() { os.Rename(backup, systemComposer) })
	} else {
		t.Cleanup(func() { os.Remove(systemComposer) })
	}

	if err := os.WriteFile(systemComposer, []byte(script), 0755); err != nil {
		t.Fatalf("write composer stub: %v", err)
	}
}

// mustReadFile reads path and returns its contents as a string.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// assertExecutable asserts that path exists and has at least one executable bit set.
func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("%s exists but is not executable (mode=%v)", path, info.Mode())
	}
}
