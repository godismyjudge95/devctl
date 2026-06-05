package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/httplog"
)

// Mago is the Tool definition for mago, a toolchain for PHP development.
//
// mago is installed as {binDir}/mago.
//
// Releases: https://github.com/carthage-software/mago/releases
var Mago = Tool{
	Name:             "mago",
	LatestRelease:    fetchMagoLatestRelease,
	DownloadTo:       downloadMagoBinary,
	InstalledVersion: installedMagoVersion,
}

// fetchMagoLatestRelease queries the GitHub Releases API for the latest mago
// release and returns the linux/x86_64 pre-compiled binary URL.
func fetchMagoLatestRelease(ctx context.Context) (Release, error) {
	tag, err := fetchGitHubTag(ctx, "carthage-software/mago")
	if err != nil {
		return Release{}, fmt.Errorf("mago: %w", err)
	}

	// mago tags have no leading "v" (e.g. "1.20.1"), so the tag is used
	// as-is for both the Version field and the download URL.
	downloadURL := fmt.Sprintf(
		"https://github.com/carthage-software/mago/releases/download/%s/mago-%s-x86_64-unknown-linux-gnu.tar.gz",
		tag, tag,
	)

	return Release{
		Version:     tag,
		DownloadURL: downloadURL,
	}, nil
}

// downloadMagoBinary downloads the mago tar.gz from rel.DownloadURL and
// extracts the mago binary to destPath.
func downloadMagoBinary(ctx context.Context, rel Release, destPath string) error {
	tmpTar := destPath + ".tar.gz"
	defer os.Remove(tmpTar)

	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	done := httplog.LogGitHubCurlDownloadStart(rel.DownloadURL)
	cmd := exec.CommandContext(dlCtx, "curl", "-fsSL", "-o", tmpTar, rel.DownloadURL)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		done(err)
		return fmt.Errorf("curl %s: %w\n%s", rel.DownloadURL, err, buf.String())
	}
	done(nil)

	return extractBinaryFromTarGz(tmpTar, "mago", destPath)
}

// installedMagoVersion runs {binPath} --version and returns the version string
// (e.g. "1.20.1"). Returns "" if the binary is absent or fails to run.
func installedMagoVersion(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	// Output: "mago 1.20.1"
	// We want the second space-delimited token.
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}
