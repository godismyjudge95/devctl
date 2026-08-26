package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WPCLI is the Tool definition for WP-CLI, the WordPress command-line interface.
//
// Installed as {binDir}/wp (a PHP phar). Requires php on PATH to run.
//
// Releases: https://github.com/wp-cli/wp-cli/releases
var WPCLI = Tool{
	Name:             "wp",
	Label:            "WP-CLI",
	Description:      "WordPress command-line interface",
	Homepage:         "https://github.com/wp-cli/wp-cli",
	LatestRelease:    fetchWPCLILatestRelease,
	DownloadTo:       downloadWPCLIBinary,
	InstalledVersion: installedWPCLIVersion,
}

func fetchWPCLILatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "wp-cli/wp-cli")
	if err != nil {
		return Release{}, fmt.Errorf("wp: %w", err)
	}
	version := strings.TrimPrefix(tag, "v")
	downloadURL := fmt.Sprintf(
		"https://github.com/wp-cli/wp-cli/releases/download/%s/wp-cli-%s.phar",
		tag, version,
	)
	return Release{Version: version, DownloadURL: downloadURL}, nil
}

func downloadWPCLIBinary(ctx context.Context, rel Release, destPath string) error {
	return downloadURL(ctx, rel.DownloadURL, destPath)
}

func installedWPCLIVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--version")
	// The phar shebang is `#!/usr/bin/env php`. Prepend the shared bin dir
	// so the managed php symlink is found even when the daemon PATH is thin.
	binDir := filepath.Dir(binPath)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// "WP-CLI 2.12.0"
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if strings.EqualFold(f, "WP-CLI") && i+1 < len(fields) {
			return strings.TrimPrefix(fields[i+1], "v")
		}
	}
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v")
}
