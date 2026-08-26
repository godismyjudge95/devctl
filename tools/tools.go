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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/danielgormly/devctl/internal/githubapi"
	"github.com/danielgormly/devctl/internal/httplog"
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

	// Label is the human-readable display name (e.g. "SQLite"). Falls back to
	// Name when empty.
	Label string

	// Description is a one-line summary shown in the install picker.
	Description string

	// Homepage is the upstream project URL.
	Homepage string

	// Aliases is an optional list of extra names that should be symlinked to
	// the installed binary inside binDir (e.g. ["nvm"] for fnm). Symlinks are
	// created (or refreshed) every time EnsureLatest runs, even when the binary
	// is already up-to-date.
	Aliases []string

	// Default marks a tool that is installed on first setup and cannot be
	// uninstalled from the catalog (sqlite3).
	Default bool

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

// State is the live catalog row returned by the API.
type State struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	Homepage        string   `json:"homepage"`
	Aliases         []string `json:"aliases,omitempty"`
	Installed       bool     `json:"installed"`
	Default         bool     `json:"default"`
	Version         string   `json:"version"`
	LatestVersion   string   `json:"latest_version"`
	UpdateAvailable bool     `json:"update_available"`
}

// AllTools is the ordered list of every tool devctl manages. Install/update
// routines iterate this slice so registering here is sufficient.
var AllTools = []Tool{
	SQLite3,
	Mago,
	PHPantom,
	WPCLI,
	FNM,
	YQ,
}

// ---------------------------------------------------------------------------
// Shared network helpers
// ---------------------------------------------------------------------------

// fetchGitHubTag queries the GitHub Releases API for the latest release tag of
// ownerRepo (e.g. "Schniz/fnm") and returns the raw tag_name (e.g. "v1.39.0").
func fetchGitHubTag(ctx context.Context, ownerRepo string) (string, error) {
	return githubapi.LatestTag(ctx, ownerRepo)
}

// downloadURL curls url into destPath.
func downloadURL(ctx context.Context, url, destPath string) error {
	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	done := httplog.LogGitHubCurlDownloadStart(url)
	cmd := exec.CommandContext(dlCtx, "curl", "-fsSL", "-o", destPath, url)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		done(err)
		return fmt.Errorf("curl %s: %w\n%s", url, err, buf.String())
	}
	done(nil)
	return nil
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

// EnsureAllLatest installs default tools (sqlite3) and updates any other tool
// whose binary is already present. Opt-in tools that have never been installed
// are left alone.
func EnsureAllLatest(ctx context.Context, binDir string, w io.Writer) {
	ensureAll(ctx, AllTools, binDir, w)
}

func ensureAll(ctx context.Context, list []Tool, binDir string, w io.Writer) {
	done := map[string]bool{}
	for _, t := range list {
		if !t.Default {
			continue
		}
		if err := EnsureLatest(ctx, t, binDir, w); err != nil {
			fmt.Fprintf(w, "warning: %v\n", err)
		}
		done[t.Name] = true
	}
	for _, t := range list {
		if done[t.Name] {
			continue
		}
		if !isPresent(t, binDir) {
			continue
		}
		if err := EnsureLatest(ctx, t, binDir, w); err != nil {
			fmt.Fprintf(w, "warning: %v\n", err)
		}
	}
}
