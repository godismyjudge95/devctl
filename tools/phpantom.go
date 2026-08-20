package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// PHPantom is the Tool definition for phpantom_lsp, a PHP language server.
//
// Releases: https://github.com/PHPantom-dev/phpantom_lsp/releases
var PHPantom = Tool{
	Name:             "phpantom_lsp",
	Label:            "PHPantom",
	Description:      "Fast PHP language server with Laravel and type intelligence",
	Homepage:         "https://github.com/PHPantom-dev/phpantom_lsp",
	LatestRelease:    fetchPHPantomLatestRelease,
	DownloadTo:       downloadPHPantomBinary,
	InstalledVersion: installedPHPantomVersion,
}

func fetchPHPantomLatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "PHPantom-dev/phpantom_lsp")
	if err != nil {
		return Release{}, fmt.Errorf("phpantom_lsp: %w", err)
	}
	version := strings.TrimPrefix(tag, "v")
	downloadURL := fmt.Sprintf(
		"https://github.com/PHPantom-dev/phpantom_lsp/releases/download/%s/phpantom_lsp-x86_64-unknown-linux-gnu.tar.gz",
		tag,
	)
	return Release{Version: version, DownloadURL: downloadURL}, nil
}

func downloadPHPantomBinary(ctx context.Context, rel Release, destPath string) error {
	tmpTar := destPath + ".tar.gz"
	defer os.Remove(tmpTar)
	if err := downloadURL(ctx, rel.DownloadURL, tmpTar); err != nil {
		return err
	}
	return extractBinaryFromTarGz(tmpTar, "phpantom_lsp", destPath)
}

func installedPHPantomVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) >= 2 {
		return strings.TrimPrefix(fields[1], "v")
	}
	if len(fields) == 1 {
		return strings.TrimPrefix(fields[0], "v")
	}
	return ""
}
