package php

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/httplog"
	"github.com/danielgormly/devctl/internal/runuser"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/tools"
)

const (
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

	cliURL, fpmURL, manifest, err := AssetURLsForMinor(ctx, ver)
	if err != nil {
		return fmt.Errorf("php %s: resolve release assets: %w", ver, err)
	}

	// 3. Download both binaries next to the current ones and prove the CLI
	//    starts before either replaces a working install.
	if err := installPHPBinary(ctx, fpmURL, fpmBin+".new", "php-fpm"); err != nil {
		return fmt.Errorf("php %s: download fpm: %w", ver, err)
	}
	defer os.Remove(fpmBin + ".new")
	if err := installPHPBinary(ctx, cliURL, cliBin+".new", "php"); err != nil {
		return fmt.Errorf("php %s: download cli: %w", ver, err)
	}
	defer os.Remove(cliBin + ".new")
	for _, bin := range []string{fpmBin, cliBin} {
		if err := os.Chmod(bin+".new", 0755); err != nil {
			return fmt.Errorf("php %s: chmod %s: %w", ver, filepath.Base(bin), err)
		}
	}
	if err := probeBinary(ctx, cliBin+".new"); err != nil {
		return fmt.Errorf("php %s: %w", ver, err)
	}
	for _, bin := range []string{fpmBin, cliBin} {
		if err := os.Rename(bin+".new", bin); err != nil {
			return fmt.Errorf("php %s: install %s: %w", ver, filepath.Base(bin), err)
		}
	}

	if err := installXdebug(ctx, manifest, ver, serverRoot, phpPlatformToken()); err != nil {
		return fmt.Errorf("php %s: download xdebug: %w", ver, err)
	}

	// 5. Symlink CLI binary into {serverRoot}/bin/php{ver}.
	binDir := paths.BinDir(serverRoot)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("php %s: create bin dir: %w", ver, err)
	}
	wrapperPath := filepath.Join(binDir, "php"+ver)
	if err := writeCLIWrapper(wrapperPath, cliBin, serverRoot); err != nil {
		return fmt.Errorf("php %s: write cli wrapper: %w", ver, err)
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
	if err := tools.EnsureLatest(ctx, tools.WPCLI, binDir, io.Discard); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install wp-cli: %v\n", err)
	}

	// 10. Link global Composer tools into the bin dir, then install the
	//     Laravel installer globally for the site user.
	composerBinPath := filepath.Join(binDir, "composer")
	if err := LinkComposerGlobalBin(ctx, composerBinPath, siteUser, siteHome); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: link composer global bin: %v\n", err)
	}
	if err := InstallLaravelCLI(ctx, composerBinPath, siteUser, siteHome); err != nil {
		// Non-fatal — log and continue.
		fmt.Printf("php: install laravel cli: %v\n", err)
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

// installXdebug downloads xdebug.so when the release ships one for this
// platform. Otherwise it removes an old copy, so XdebugStatus.Available always
// describes the installed build.
func installXdebug(ctx context.Context, manifest *ReleaseManifest, ver, serverRoot, platform string) error {
	dest := XdebugPath(ver, serverRoot)
	assets, _ := manifest.Lookup(ver, platform)
	if assets.Xdebug == "" {
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return curlDownload(ctx, releaseAssetURL(manifest.ReleaseTag, assets.Xdebug), dest)
}

// probeBinary runs "php -n -v". A Linux build needs a newer glibc than old
// distributions ship, and the loader then refuses to start it.
func probeBinary(ctx context.Context, bin string) error {
	out, err := exec.CommandContext(ctx, bin, "-n", "-v").CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if strings.Contains(msg, "GLIBC_") {
		return fmt.Errorf("this PHP build does not start on this system because its glibc is too old. Use Ubuntu 24.04 or Debian 13 or newer. The installed PHP did not change.\n%s", msg)
	}
	return fmt.Errorf("the downloaded PHP does not start. The installed PHP did not change: %w\n%s", err, msg)
}

func installPHPBinary(ctx context.Context, url, dest, binName string) error {
	if !strings.Contains(url, ".tar.gz") {
		return curlDownload(ctx, url, dest)
	}
	tmp := dest + ".tar.gz"
	defer os.Remove(tmp)
	if err := curlDownload(ctx, url, tmp); err != nil {
		return err
	}
	return extractNamedFromTarGz(tmp, binName, dest)
}

func extractNamedFromTarGz(tarPath, binName, dest string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != binName {
			continue
		}
		out, err := os.OpenFile(dest+".extract", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Rename(dest+".extract", dest)
	}
	return fmt.Errorf("binary %q not found in tarball", binName)
}

// curlDownload fetches url and writes it to dest using curl.
// Follows redirects (-L) and fails on HTTP errors (-f).
// Downloads to a sibling temp file then renames into place so a running
// binary (e.g. php-fpm) can be replaced without curl exit 23 / ETXTBSY.
func curlDownload(ctx context.Context, url, dest string) error {
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("curl %s: create dest dir: %w", url, err)
	}

	tmp := dest + ".download"
	_ = os.Remove(tmp)
	defer os.Remove(tmp)

	done := httplog.LogGitHubCurlDownloadStart(url)
	cmd := exec.CommandContext(dlCtx, "curl", "-fsSL", "-o", tmp, url)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		done(err)
		return fmt.Errorf("curl %s: %w\n%s", url, err, buf.String())
	}
	done(nil)

	// Rename replaces the directory entry even when dest is a running
	// executable; writing to dest in-place would fail with ETXTBSY.
	if err := os.Rename(tmp, dest); err != nil {
		// Fallback: remove dest then rename (covers odd filesystems).
		_ = os.Remove(dest)
		if err2 := os.Rename(tmp, dest); err2 != nil {
			return fmt.Errorf("curl %s: install download: %w", url, err2)
		}
	}
	return nil
}

// disableSystemFPM stops and disables the system php{ver}-fpm.service unit so
// it does not hold /run/php/php{ver}-fpm.sock. Errors are ignored — the unit
// may not be installed on the system.
func disableSystemFPM(ctx context.Context, ver string) {
	if runtime.GOOS != "linux" {
		return
	}
	unit := fmt.Sprintf("php%s-fpm.service", ver)
	for _, action := range []string{"stop", "disable"} {
		cmd := exec.CommandContext(ctx, "systemctl", action, unit)
		_ = cmd.Run()
	}
}

const (
	composerURL = "https://getcomposer.org/composer-stable.phar"
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

// composerCmd runs Composer with its own bin dir first on PATH. Composer
// starts with "#!/usr/bin/env php", and the daemon PATH has no php on macOS
// or on a Linux without a system PHP.
func composerCmd(composerBin, args string) string {
	return "PATH='" + filepath.Dir(composerBin) + "':\"$PATH\" '" + composerBin + "' " + args
}

// LinkComposerGlobalBin sets the site user's global Composer bin-dir to the
// devctl bin dir, which is already on PATH. Tools from "composer global
// require" then run by name with no other PATH entry. When the setting
// changes, "composer global install" links the tools already installed.
func LinkComposerGlobalBin(ctx context.Context, composerBin, siteUser, siteHome string) error {
	binDir := filepath.Dir(composerBin)
	out, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerCmd(composerBin, "global config bin-dir --absolute"))
	if err != nil {
		return fmt.Errorf("composer global config bin-dir: %w\n%s", err, out)
	}
	// "global" first prints "Changed current directory to ..." on stderr.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if strings.TrimSpace(lines[len(lines)-1]) == binDir {
		return nil
	}
	if out, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerCmd(composerBin, "global config bin-dir '"+binDir+"'")); err != nil {
		return fmt.Errorf("composer global config bin-dir %s: %w\n%s", binDir, err, out)
	}
	if out, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerCmd(composerBin, "global install --no-interaction --quiet")); err != nil {
		return fmt.Errorf("composer global install: %w\n%s", err, out)
	}
	return nil
}

// InstallLaravelCLI globally installs laravel/installer via Composer as siteUser.
// The binary lands in the Composer global bin dir, {serverRoot}/bin after
// LinkComposerGlobalBin.
// composerBin is the absolute path to the Composer binary (e.g. {serverRoot}/bin/composer).
// It is safe to call on every PHP install — Composer will upgrade if a newer
// version is available.
func InstallLaravelCLI(ctx context.Context, composerBin, siteUser, siteHome string) error {
	if siteUser == "" || siteHome == "" {
		return fmt.Errorf("siteUser and siteHome must be set")
	}
	_, err := runuser.RunAsUserW(ctx, io.Discard, siteUser, siteHome, "",
		composerCmd(composerBin, "global require laravel/installer --no-interaction --quiet"))
	if err != nil {
		return fmt.Errorf("composer global require laravel/installer: %w", err)
	}
	return nil
}
