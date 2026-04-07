package php

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/runuser"
	"github.com/danielgormly/devctl/paths"
)

const (
	// ghReleaseBase is the prefix for PHP binary assets.
	// Binaries are published under the fixed "php-binaries-latest" GitHub release tag
	// so that PHP installs/updates are decoupled from regular devctl releases.
	// Binaries are named php-{ver}-{sapi}-linux-x86_64 (raw executable, no archive).
	ghReleaseBase   = "https://github.com/godismyjudge95/devctl/releases/download/php-binaries-latest/"
	downloadTimeout = 10 * time.Minute
)

// Install downloads the static PHP-FPM and CLI binaries for the given minor
// version (e.g. "8.3"), writes config files, and symlinks the CLI binary into
// {serverRoot}/bin/php{ver}.
//
// serverRoot is the absolute path to the devctl server directory
// (e.g. "/home/alice/ddev/sites/server").
// siteUser is the non-root OS username (e.g. "alice") — PHP-FPM workers run as
// this user so they can write to site storage directories.
// siteHome is the home directory of siteUser (e.g. "/home/alice") — used for
// installing Composer global tools on behalf of the site user.
// The binaries are installed to {serverRoot}/php/{ver}/.
func Install(ctx context.Context, ver string, serverRoot string, siteUser string, siteHome string) error {
	phpDir := PHPDir(ver, serverRoot)
	fpmBin := filepath.Join(phpDir, "php-fpm")
	cliBin := filepath.Join(phpDir, "php")

	// 1. Create install directory.
	if err := os.MkdirAll(phpDir, 0755); err != nil {
		return fmt.Errorf("php %s: create dir: %w", ver, err)
	}

	// 2. Stop and disable the system php{ver}-fpm unit if present, so it does
	//    not hold the conventional socket path we're about to use.
	disableSystemFPM(ctx, ver)

	// 3. Download FPM binary directly from the devctl GitHub release.
	fpmURL := ghReleaseBase + fmt.Sprintf("php-%s-fpm-linux-x86_64", ver)
	if err := curlDownload(ctx, fpmURL, fpmBin); err != nil {
		return fmt.Errorf("php %s: download fpm: %w", ver, err)
	}
	if err := os.Chmod(fpmBin, 0755); err != nil {
		return fmt.Errorf("php %s: chmod fpm: %w", ver, err)
	}

	// 4. Download CLI binary directly from the devctl GitHub release.
	cliURL := ghReleaseBase + fmt.Sprintf("php-%s-cli-linux-x86_64", ver)
	if err := curlDownload(ctx, cliURL, cliBin); err != nil {
		return fmt.Errorf("php %s: download cli: %w", ver, err)
	}
	if err := os.Chmod(cliBin, 0755); err != nil {
		return fmt.Errorf("php %s: chmod cli: %w", ver, err)
	}

	// 5. Symlink CLI binary into {serverRoot}/bin/php{ver}.
	binDir := paths.BinDir(serverRoot)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("php %s: create bin dir: %w", ver, err)
	}
	symlinkPath := filepath.Join(binDir, "php"+ver)
	_ = os.Remove(symlinkPath) // remove stale symlink if any
	if err := os.Symlink(cliBin, symlinkPath); err != nil {
		return fmt.Errorf("php %s: symlink cli: %w", ver, err)
	}

	// 6. Write php-fpm.conf and php.ini.
	if err := WriteConfigs(ver, serverRoot, siteUser); err != nil {
		return fmt.Errorf("php %s: write configs: %w", ver, err)
	}

	// 7. Configure auto_prepend_file for the dump server.
	if err := ConfigurePrepend(ctx, ver, serverRoot); err != nil {
		// Non-fatal.
		fmt.Printf("php: configure prepend for %s: %v\n", ver, err)
	}

	// 8. Update {serverRoot}/bin/php to point at the highest installed version.
	if err := UpdateGlobalSymlink(serverRoot); err != nil {
		fmt.Printf("php: %v\n", err)
	}

	// 9. Download/update Composer and WP-CLI into the shared bin dir.
	if err := InstallComposer(ctx, binDir); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install composer: %v\n", err)
	}
	if err := InstallWPCLI(ctx, binDir); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install wp-cli: %v\n", err)
	}

	// 10. Install Laravel and Statamic CLIs globally via Composer for the site user.
	composerBinPath := filepath.Join(binDir, "composer")
	if err := InstallLaravelCLI(ctx, composerBinPath, siteUser, siteHome); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install laravel cli: %v\n", err)
	}
	if err := InstallStatamicCLI(ctx, composerBinPath, siteUser, siteHome); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install statamic cli: %v\n", err)
	}

	return nil
}

// Uninstall stops the FPM process (caller responsibility), removes the symlink,
// and deletes the install directory.
func Uninstall(ctx context.Context, ver string, serverRoot string) error {
	// Remove versioned CLI symlink from {serverRoot}/bin/php{ver}.
	symlinkPath := filepath.Join(paths.BinDir(serverRoot), "php"+ver)
	if err := os.Remove(symlinkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("php %s: remove symlink: %w", ver, err)
	}

	// Remove install directory.
	phpDir := PHPDir(ver, serverRoot)
	if err := os.RemoveAll(phpDir); err != nil {
		return fmt.Errorf("php %s: remove dir: %w", ver, err)
	}

	// Update {serverRoot}/bin/php to point at the new highest installed version.
	if err := UpdateGlobalSymlink(serverRoot); err != nil {
		fmt.Printf("php: %v\n", err)
	}

	return nil
}

// curlDownload fetches url and writes it to dest using curl.
// Follows redirects (-L) and fails on HTTP errors (-f).
func curlDownload(ctx context.Context, url, dest string) error {
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	cmd := exec.CommandContext(dlCtx, "curl", "-fsSL", "-o", dest, url)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("curl %s: %w\n%s", url, err, buf.String())
	}
	return nil
}

// disableSystemFPM stops and disables the system php{ver}-fpm.service unit so
// it does not hold /run/php/php{ver}-fpm.sock. Errors are ignored — the unit
// may not be installed on the system.
func disableSystemFPM(ctx context.Context, ver string) {
	unit := fmt.Sprintf("php%s-fpm.service", ver)
	for _, action := range []string{"stop", "disable"} {
		cmd := exec.CommandContext(ctx, "systemctl", action, unit)
		_ = cmd.Run()
	}
}

const (
	composerURL = "https://getcomposer.org/composer-stable.phar"
	wpcliURL    = "https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"
)

// InstallComposer downloads the latest stable Composer phar into binDir as
// "composer" and makes it executable. It is safe to call on every PHP install
// — it always refreshes to the latest build.
func InstallComposer(ctx context.Context, binDir string) error {
	dest := filepath.Join(binDir, "composer")
	if err := curlDownload(ctx, composerURL, dest); err != nil {
		return fmt.Errorf("download composer: %w", err)
	}
	if err := os.Chmod(dest, 0755); err != nil {
		return fmt.Errorf("chmod composer: %w", err)
	}
	return nil
}

// InstallWPCLI downloads the latest WP-CLI phar into binDir as "wp" and makes
// it executable. It is safe to call on every PHP install — it always refreshes
// to the latest build.
func InstallWPCLI(ctx context.Context, binDir string) error {
	dest := filepath.Join(binDir, "wp")
	if err := curlDownload(ctx, wpcliURL, dest); err != nil {
		return fmt.Errorf("download wp-cli: %w", err)
	}
	if err := os.Chmod(dest, 0755); err != nil {
		return fmt.Errorf("chmod wp-cli: %w", err)
	}
	return nil
}

// ComposerGlobalBinDir returns the absolute path to the Composer global bin
// directory for the given user. It runs `composer global config bin-dir
// --absolute` as siteUser to discover the actual path. If that fails (e.g.
// Composer is not yet installed), it falls back to the XDG default:
//
//	{siteHome}/.config/composer/vendor/bin
func ComposerGlobalBinDir(ctx context.Context, composerBin, siteUser, siteHome string) string {
	if siteUser == "" || siteHome == "" {
		return filepath.Join(siteHome, ".config", "composer", "vendor", "bin")
	}
	out, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerBin+" global config bin-dir --absolute")
	if err == nil {
		dir := strings.TrimSpace(out)
		if dir != "" {
			return dir
		}
	}
	// Fall back to XDG default.
	return filepath.Join(siteHome, ".config", "composer", "vendor", "bin")
}

// InstallLaravelCLI globally installs laravel/installer via Composer as siteUser.
// The binary lands in the Composer global bin directory (typically
// {siteHome}/.config/composer/vendor/bin/laravel).
// composerBin is the absolute path to the Composer binary (e.g. {serverRoot}/bin/composer).
// It is safe to call on every PHP install — Composer will upgrade if a newer
// version is available.
func InstallLaravelCLI(ctx context.Context, composerBin, siteUser, siteHome string) error {
	if siteUser == "" || siteHome == "" {
		return fmt.Errorf("siteUser and siteHome must be set")
	}
	_, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerBin+" global require laravel/installer --no-interaction --quiet")
	if err != nil {
		return fmt.Errorf("composer global require laravel/installer: %w", err)
	}
	return nil
}

// InstallStatamicCLI globally installs statamic/cli via Composer as siteUser.
// The binary lands in the Composer global bin directory (typically
// {siteHome}/.config/composer/vendor/bin/statamic).
// composerBin is the absolute path to the Composer binary (e.g. {serverRoot}/bin/composer).
// It is safe to call on every PHP install — Composer will upgrade if a newer
// version is available.
func InstallStatamicCLI(ctx context.Context, composerBin, siteUser, siteHome string) error {
	if siteUser == "" || siteHome == "" {
		return fmt.Errorf("siteUser and siteHome must be set")
	}
	_, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerBin+" global require statamic/cli --no-interaction --quiet")
	if err != nil {
		return fmt.Errorf("composer global require statamic/cli: %w", err)
	}
	return nil
}
