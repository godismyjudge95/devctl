// Package tools manages the download and update of optional CLI tools that
// devctl installs into the shared bin directory (paths.BinDir). Each Tool
// definition knows how to detect the installed version, fetch the latest
// upstream release, and download + install the binary.
//
// Adding a new tool is a two-step process:
//  1. Create a file (e.g. tools/mytool.go) that defines a var of type Tool.
//  2. Register it in AllTools so it is included in bulk install/update runs.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Release describes a specific version of a downloadable tool.
type Release struct {
	// Version is the human-readable version string (e.g. "3.46.1").
	Version string
	// VersionInt is a monotonically increasing integer for comparison
	// (e.g. 3460100 for SQLite 3.46.1). May be zero when not applicable.
	VersionInt int
	// DownloadURL is the full URL to the release archive.
	DownloadURL string
}

// Tool describes a downloadable CLI tool managed by devctl.
type Tool struct {
	// Name is the binary name as it appears in the bin dir (e.g. "sqlite3").
	Name string

	// Aliases is an optional list of extra names that should be symlinked to
	// the installed binary inside binDir (e.g. ["nvm"] for fnm). Symlinks are
	// created (or refreshed) every time EnsureLatest runs, even when the binary
	// is already up-to-date.
	Aliases []string

	// LatestRelease fetches metadata about the latest upstream release.
	LatestRelease func(ctx context.Context) (Release, error)

	// DownloadTo downloads and unpacks the release binary to destPath
	// (a full file path, not a directory). The file at destPath is replaced
	// atomically on success.
	DownloadTo func(ctx context.Context, rel Release, destPath string) error

	// InstalledVersion returns the version string of the currently installed
	// binary at binPath (e.g. "3.46.1"). Returns "" if absent or not runnable.
	InstalledVersion func(ctx context.Context, binPath string) string
}

// AllTools is the ordered list of every tool devctl manages. Install/update
// routines iterate this slice so registering here is sufficient.
var AllTools = []Tool{
	SQLite3,
	FNM,
}

// ---------------------------------------------------------------------------
// Shared network helpers
// ---------------------------------------------------------------------------

// fetchGitHubTag queries the GitHub Releases API for the latest release tag of
// ownerRepo (e.g. "Schniz/fnm") and returns the raw tag_name (e.g. "v1.39.0").
func fetchGitHubTag(ctx context.Context, ownerRepo string) (string, error) {
	url := "https://api.github.com/repos/" + ownerRepo + "/releases/latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github version check %s: HTTP %d", ownerRepo, resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("github version check %s: decode: %w", ownerRepo, err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("github version check %s: empty tag_name", ownerRepo)
	}
	return payload.TagName, nil
}

// EnsureLatest checks whether the tool at {binDir}/{tool.Name} is already at
// the latest upstream version. If not (or if absent), it downloads and
// installs the latest release. Alias symlinks are always refreshed regardless
// of whether a download was needed.
//
// Progress (step label + result) is written to w. Errors are returned so the
// caller can decide whether to abort or continue.
func EnsureLatest(ctx context.Context, t Tool, binDir string, w io.Writer) error {
	binPath := filepath.Join(binDir, t.Name)

	rel, err := t.LatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("%s: fetch latest version: %w", t.Name, err)
	}

	current := t.InstalledVersion(ctx, binPath)
	if current != rel.Version {
		fmt.Fprintf(w, "Downloading %s %s...\n", t.Name, rel.Version)

		if err := os.MkdirAll(binDir, 0755); err != nil {
			return fmt.Errorf("%s: create bin dir: %w", t.Name, err)
		}

		// Download to a temp path inside binDir (same filesystem → atomic rename).
		tmpPath := binPath + ".download"
		defer os.Remove(tmpPath) // clean up if something goes wrong

		if err := t.DownloadTo(ctx, rel, tmpPath); err != nil {
			return fmt.Errorf("%s: download %s: %w", t.Name, rel.Version, err)
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			return fmt.Errorf("%s: chmod: %w", t.Name, err)
		}

		if err := os.Rename(tmpPath, binPath); err != nil {
			return fmt.Errorf("%s: install: %w", t.Name, err)
		}
	}

	// Always ensure alias symlinks exist (idempotent — refresh on every run).
	for _, alias := range t.Aliases {
		aliasPath := filepath.Join(binDir, alias)
		_ = os.Remove(aliasPath)
		if err := os.Symlink(binPath, aliasPath); err != nil {
			fmt.Fprintf(w, "warning: %s: create alias %s: %v\n", t.Name, alias, err)
		}
	}

	return nil
}

// EnsureAllLatest calls EnsureLatest for every tool in AllTools, writing a
// labelled step to w for each one. Individual tool errors are printed to w as
// warnings rather than aborting the whole run.
func EnsureAllLatest(ctx context.Context, binDir string, w io.Writer) {
	for _, t := range AllTools {
		if err := EnsureLatest(ctx, t, binDir, w); err != nil {
			fmt.Fprintf(w, "warning: %v\n", err)
		}
	}
}
