package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// YQ is the Tool definition for mikefarah/yq, a YAML/JSON/XML processor.
//
// Releases: https://github.com/mikefarah/yq/releases
var YQ = Tool{
	Name:             "yq",
	Label:            "yq",
	Description:      "Command-line YAML, JSON, and XML processor",
	Homepage:         "https://github.com/mikefarah/yq",
	LatestRelease:    fetchYQLatestRelease,
	DownloadTo:       downloadYQBinary,
	InstalledVersion: installedYQVersion,
}

func fetchYQLatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "mikefarah/yq")
	if err != nil {
		return Release{}, fmt.Errorf("yq: %w", err)
	}
	version := strings.TrimPrefix(tag, "v")
	downloadURL := fmt.Sprintf(
		"https://github.com/mikefarah/yq/releases/download/%s/yq_linux_amd64",
		tag,
	)
	return Release{Version: version, DownloadURL: downloadURL}, nil
}

func downloadYQBinary(ctx context.Context, rel Release, destPath string) error {
	return downloadURL(ctx, rel.DownloadURL, destPath)
}

func installedYQVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	// "yq (https://github.com/mikefarah/yq/) version v4.53.6"
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v")
}
