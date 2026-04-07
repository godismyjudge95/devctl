// Package selfupdate handles devctl self-update from GitHub releases.
//
// It fetches the latest version tag from the GitHub Releases API, downloads
// the new binary, verifies it runs, backs up the current binary, and replaces
// it atomically. The caller is responsible for restarting the service.
package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	githubRepo      = "godismyjudge95/devctl"
	downloadTimeout = 10 * time.Minute
)

// githubAPIBase and githubDownloadBase are vars so they can be overridden in
// tests (via DEVCTL_GITHUB_API_BASE and DEVCTL_GITHUB_DOWNLOAD_BASE env vars
// checked at call-time, or by setting the vars directly in the test binary).
var (
	GithubAPIBase      = "https://api.github.com"
	GithubDownloadBase = "https://github.com/godismyjudge95/devctl/releases/download"
)

// LatestVersion queries the GitHub Releases API and returns the latest release
// tag for devctl (e.g. "v0.3.0"). Returns ("", nil) if the version cannot be
// determined.
func LatestVersion(ctx context.Context) (string, error) {
	url := GithubAPIBase + "/repos/" + githubRepo + "/releases/latest"

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("selfupdate: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("selfupdate: github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfupdate: github api: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("selfupdate: decode response: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("selfupdate: empty tag_name in response")
	}
	return payload.TagName, nil
}

// Update downloads the devctl binary for the given version, verifies it
// executes, backs up the current binary, and replaces it atomically.
//
// currentBinaryPath is the absolute path to the currently-running devctl
// binary (from os.Executable). version is the release tag to install
// (e.g. "v0.3.0"). Progress messages are written to w.
//
// After Update returns successfully the caller should restart the service.
// The old binary is preserved at currentBinaryPath+".bak" until the next
// successful startup (see CleanupBackup).
func Update(ctx context.Context, currentBinaryPath, version string, w io.Writer) error {
	fmt.Fprintf(w, "Updating devctl from %s to %s...\n", currentBinaryRunningVersion(currentBinaryPath), version)

	// 1. Create a temp directory. Download the binary to a file named "devctl"
	//    inside it so the curl shim in the test environment can serve it from
	//    the artifact cache (the shim matches on basename of the -o destination).
	tmpDir, err := os.MkdirTemp("", "devctl-update-*")
	if err != nil {
		return fmt.Errorf("selfupdate: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpBin := filepath.Join(tmpDir, "devctl")
	downloadURL := GithubDownloadBase + "/" + version + "/devctl"

	fmt.Fprintf(w, "Downloading %s...\n", downloadURL)
	if err := curlDownload(ctx, downloadURL, tmpBin); err != nil {
		return fmt.Errorf("selfupdate: download: %w", err)
	}

	// 2. Make executable.
	if err := os.Chmod(tmpBin, 0755); err != nil {
		return fmt.Errorf("selfupdate: chmod: %w", err)
	}

	// 3. Verify the downloaded binary runs and reports a version.
	fmt.Fprintf(w, "Verifying downloaded binary...\n")
	verCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(verCtx, tmpBin, "--version").Output()
	if err != nil {
		return fmt.Errorf("selfupdate: verify binary: binary refuses to run: %w", err)
	}
	fmt.Fprintf(w, "Downloaded binary reports version: %s\n", bytes.TrimSpace(out))

	// 4. Back up the current binary so we can restore it if the update fails.
	backupPath := currentBinaryPath + ".bak"
	fmt.Fprintf(w, "Backing up current binary to %s...\n", backupPath)
	if err := copyFile(currentBinaryPath, backupPath); err != nil {
		return fmt.Errorf("selfupdate: backup: %w", err)
	}

	// 5. Replace the current binary atomically.
	//    os.Rename across filesystems would fail, so we copy then rename within
	//    the same directory (temp dir may be on /tmp which is a different fs).
	fmt.Fprintf(w, "Installing new binary...\n")
	if err := copyFile(tmpBin, currentBinaryPath); err != nil {
		// Attempt to restore backup before returning.
		if restoreErr := copyFile(backupPath, currentBinaryPath); restoreErr != nil {
			fmt.Fprintf(w, "WARNING: failed to restore backup: %v\n", restoreErr)
		}
		return fmt.Errorf("selfupdate: install: %w", err)
	}

	fmt.Fprintf(w, "Update complete — restarting service...\n")
	return nil
}

// CleanupBackup removes {binaryPath}.bak if it exists. This should be called
// on successful startup to confirm the previous update succeeded.
func CleanupBackup(binaryPath string) {
	backupPath := binaryPath + ".bak"
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Remove(backupPath); err == nil {
			// Logged by the caller.
		}
	}
}

// currentBinaryRunningVersion tries to read the version of the current binary
// by running it with --version. Returns "unknown" on failure.
func currentBinaryRunningVersion(binaryPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binaryPath, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return string(bytes.TrimSpace(out))
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

// copyFile copies src to dst atomically using a temp file in the same
// directory as dst. Preserves the source file's permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
