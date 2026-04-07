package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// FNM is the Tool definition for fnm (Fast Node Manager), a fast
// Node.js version manager written in Rust.
//
// fnm is installed as {binDir}/fnm. A symlink {binDir}/nvm → {binDir}/fnm is
// also created so the binary is reachable under the familiar "nvm" name.
//
// Releases: https://github.com/Schniz/fnm/releases
var FNM = Tool{
	Name:             "fnm",
	Aliases:          []string{"nvm"},
	LatestRelease:    fetchFNMLatestRelease,
	DownloadTo:       downloadFNMBinary,
	InstalledVersion: installedFNMVersion,
}

// fetchFNMLatestRelease queries the GitHub Releases API for the latest fnm
// release and returns the linux/x64 pre-compiled binary URL.
func fetchFNMLatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "Schniz/fnm")
	if err != nil {
		return Release{}, fmt.Errorf("fnm: %w", err)
	}

	// Strip leading "v" for the Version field so it matches `fnm --version`
	// output (e.g. tag "v1.39.0" → version "1.39.0").
	version := strings.TrimPrefix(tag, "v")
	downloadURL := fmt.Sprintf(
		"https://github.com/Schniz/fnm/releases/download/%s/fnm-linux.zip",
		tag,
	)

	return Release{
		Version:     version,
		DownloadURL: downloadURL,
	}, nil
}

// downloadFNMBinary downloads fnm-linux.zip from rel.DownloadURL and extracts
// the fnm binary to destPath.
func downloadFNMBinary(ctx context.Context, rel Release, destPath string) error {
	tmpZip := destPath + ".zip"
	defer os.Remove(tmpZip)

	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(dlCtx, "curl", "-fsSL", "-o", tmpZip, rel.DownloadURL)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("curl %s: %w\n%s", rel.DownloadURL, err, buf.String())
	}

	return extractBinaryFromZip(tmpZip, "fnm", destPath)
}

// installedFNMVersion runs {binPath} --version and returns the version string
// (e.g. "1.39.0"). Returns "" if the binary is absent or fails to run.
func installedFNMVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	// Output: "fnm 1.39.0"
	// We want the second space-delimited token.
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}
