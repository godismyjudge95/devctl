package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/danielgormly/devctl/dist"
)

// Cloudflared is the Tool definition for cloudflared, the Cloudflare Tunnel
// client. devctl uses it for quick tunnels (sites:share).
//
// Releases: https://github.com/cloudflare/cloudflared/releases
var Cloudflared = Tool{
	Name:             "cloudflared",
	Label:            "cloudflared",
	Description:      "Cloudflare Tunnel client, used to share a site on a public URL",
	Homepage:         "https://github.com/cloudflare/cloudflared",
	LatestRelease:    fetchCloudflaredLatestRelease,
	DownloadTo:       downloadCloudflaredBinary,
	InstalledVersion: installedCloudflaredVersion,
}

func fetchCloudflaredLatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "cloudflare/cloudflared")
	if err != nil {
		return Release{}, fmt.Errorf("cloudflared: %w", err)
	}
	return cloudflaredRelease(tag)
}

func cloudflaredRelease(tag string) (Release, error) {
	a, err := dist.For("cloudflared")
	if err != nil {
		return Release{}, err
	}
	return Release{
		Version:     tag,
		DownloadURL: fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/download/%s/%s", tag, a.File),
	}, nil
}

// downloadCloudflaredBinary handles both release shapes: a bare binary on
// Linux and a .tgz on macOS.
func downloadCloudflaredBinary(ctx context.Context, rel Release, destPath string) error {
	if !strings.HasSuffix(rel.DownloadURL, ".tgz") {
		return downloadURL(ctx, rel.DownloadURL, destPath)
	}
	tmp := destPath + ".tgz"
	defer os.Remove(tmp)
	if err := downloadURL(ctx, rel.DownloadURL, tmp); err != nil {
		return err
	}
	return extractBinaryFromTarGz(tmp, "cloudflared", destPath)
}

func installedCloudflaredVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	return parseCloudflaredVersion(string(out))
}

// parseCloudflaredVersion reads "cloudflared version 2026.10.7 (built ...)".
func parseCloudflaredVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) < 3 || fields[1] != "version" {
		return ""
	}
	return fields[2]
}
