package php

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	staticPHPIndex  = "https://dl.static-php.dev/static-php-cli/common/"
	downloadTimeout = 10 * time.Minute
)

// Install downloads the static PHP-FPM and CLI binaries for the given minor
// version (e.g. "8.3"), writes config files, and symlinks the CLI binary into
// /usr/local/bin/php{ver}.
//
// siteHome is the home directory of the non-root site user (e.g. "/home/alice").
// The binaries are installed to {siteHome}/sites/server/php/{ver}/.
func Install(ctx context.Context, ver string, siteHome string) error {
	fullVer, err := resolveFullVersion(ctx, ver)
	if err != nil {
		return fmt.Errorf("php %s: resolve version: %w", ver, err)
	}

	phpDir := PHPDir(ver, siteHome)
	fpmBin := filepath.Join(phpDir, "php-fpm")
	cliBin := filepath.Join(phpDir, "php")

	// 1. Create install directory.
	if err := os.MkdirAll(phpDir, 0755); err != nil {
		return fmt.Errorf("php %s: create dir: %w", ver, err)
	}

	// 2. Ensure /run/php/ exists (tmpfs — may vanish after reboot, FPM recreates
	//    the socket but the directory must exist first).
	if err := os.MkdirAll("/run/php", 0755); err != nil {
		return fmt.Errorf("php %s: create /run/php: %w", ver, err)
	}

	// 3. Stop and disable the system php{ver}-fpm unit if present, so it does
	//    not hold the conventional socket path we're about to use.
	disableSystemFPM(ctx, ver)

	// 4. Download and extract FPM binary.
	fpmURL := fmt.Sprintf("%sphp-%s-fpm-linux-x86_64.tar.gz", staticPHPIndex, fullVer)
	tmpFPM := filepath.Join(os.TempDir(), fmt.Sprintf("php-%s-fpm.tar.gz", fullVer))
	defer os.Remove(tmpFPM)

	if err := curlDownload(ctx, fpmURL, tmpFPM); err != nil {
		return fmt.Errorf("php %s: download fpm: %w", ver, err)
	}
	if err := extractFromTar(tmpFPM, "php-fpm", fpmBin); err != nil {
		return fmt.Errorf("php %s: extract fpm: %w", ver, err)
	}
	if err := os.Chmod(fpmBin, 0755); err != nil {
		return fmt.Errorf("php %s: chmod fpm: %w", ver, err)
	}

	// 5. Download and extract CLI binary.
	cliURL := fmt.Sprintf("%sphp-%s-cli-linux-x86_64.tar.gz", staticPHPIndex, fullVer)
	tmpCLI := filepath.Join(os.TempDir(), fmt.Sprintf("php-%s-cli.tar.gz", fullVer))
	defer os.Remove(tmpCLI)

	if err := curlDownload(ctx, cliURL, tmpCLI); err != nil {
		return fmt.Errorf("php %s: download cli: %w", ver, err)
	}
	if err := extractFromTar(tmpCLI, "php", cliBin); err != nil {
		return fmt.Errorf("php %s: extract cli: %w", ver, err)
	}
	if err := os.Chmod(cliBin, 0755); err != nil {
		return fmt.Errorf("php %s: chmod cli: %w", ver, err)
	}

	// 6. Symlink CLI binary to /usr/local/bin/php{ver}.
	symlinkPath := "/usr/local/bin/php" + ver
	_ = os.Remove(symlinkPath) // remove stale symlink if any
	if err := os.Symlink(cliBin, symlinkPath); err != nil {
		return fmt.Errorf("php %s: symlink cli: %w", ver, err)
	}

	// 7. Write php-fpm.conf and php.ini.
	if err := WriteConfigs(ver, siteHome); err != nil {
		return fmt.Errorf("php %s: write configs: %w", ver, err)
	}

	// 8. Configure auto_prepend_file for the dump server.
	if err := ConfigurePrepend(ctx, ver, siteHome); err != nil {
		// Non-fatal.
		fmt.Printf("php: configure prepend for %s: %v\n", ver, err)
	}

	// 9. Update /usr/local/bin/php to point at the highest installed version.
	if err := UpdateGlobalSymlink(siteHome); err != nil {
		fmt.Printf("php: %v\n", err)
	}

	return nil
}

// Uninstall stops the FPM process (caller responsibility), removes the symlink,
// and deletes the install directory.
func Uninstall(ctx context.Context, ver string, siteHome string) error {
	// Remove CLI symlink.
	symlinkPath := "/usr/local/bin/php" + ver
	if err := os.Remove(symlinkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("php %s: remove symlink: %w", ver, err)
	}

	// Remove install directory.
	phpDir := PHPDir(ver, siteHome)
	if err := os.RemoveAll(phpDir); err != nil {
		return fmt.Errorf("php %s: remove dir: %w", ver, err)
	}

	// Update /usr/local/bin/php to point at the new highest installed version.
	if err := UpdateGlobalSymlink(siteHome); err != nil {
		fmt.Printf("php: %v\n", err)
	}

	return nil
}

// staticPHPEntry is one item from the ?format=json directory listing.
type staticPHPEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// resolveFullVersion fetches the static-php.dev JSON index and returns the
// latest available patch version for the given minor version
// (e.g. "8.3" → "8.3.30").
func resolveFullVersion(ctx context.Context, minor string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, staticPHPIndex+"?format=json", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()

	var entries []staticPHPEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", fmt.Errorf("decode index: %w", err)
	}

	// Match filenames like: php-8.3.30-fpm-linux-x86_64.tar.gz
	pattern := regexp.MustCompile(`^php-(` + regexp.QuoteMeta(minor) + `\.\d+)-fpm-linux-x86_64\.tar\.gz$`)
	var latest string
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if m := pattern.FindStringSubmatch(e.Name); m != nil {
			latest = m[1]
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no builds found for PHP %s", minor)
	}
	return latest, nil
}

// curlDownload fetches url and writes it to dest using curl.
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

// extractFromTar finds the first entry whose base name matches binaryName in a
// .tar.gz archive and writes it to destPath.
func extractFromTar(tarPath, binaryName, destPath string) error {
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
		if hdr.Typeflag == tar.TypeReg && strings.TrimSuffix(filepath.Base(hdr.Name), ".exe") == binaryName {
			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("%s not found in archive", binaryName)
}
