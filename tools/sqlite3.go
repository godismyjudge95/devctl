package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// sqlitePathRe matches the relative download path for the linux/x64 tools
	// bundle, e.g. "2024/sqlite-tools-linux-x64-3460100.zip", capturing
	// sub-match 1 = full path, sub-match 2 = version integer string.
	sqlitePathRe = regexp.MustCompile(`(\d{4}/sqlite-tools-linux-x64-(\d+)\.zip)`)
)

// SQLite3 is the Tool definition for the official SQLite3 CLI binary.
// It downloads the pre-compiled linux/x64 tools bundle from sqlite.org,
// extracts the sqlite3 binary, and places it in the bin dir.
var SQLite3 = Tool{
	Name:             "sqlite3",
	LatestRelease:    fetchSQLite3LatestRelease,
	DownloadTo:       downloadSQLite3Binary,
	InstalledVersion: installedSQLite3Version,
}

// fetchSQLite3LatestRelease scrapes https://www.sqlite.org/download.html and
// returns the latest pre-compiled linux/x64 release.
func fetchSQLite3LatestRelease(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.sqlite.org/download.html", nil)
	if err != nil {
		return Release{}, fmt.Errorf("sqlite3: build request: %w", err)
	}
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("sqlite3: fetch download page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("sqlite3: download page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Release{}, fmt.Errorf("sqlite3: read download page: %w", err)
	}

	match := sqlitePathRe.FindSubmatch(body)
	if match == nil {
		return Release{}, fmt.Errorf("sqlite3: linux/x64 tools zip not found on download page")
	}

	relPath := string(match[1])       // e.g. "2024/sqlite-tools-linux-x64-3460100.zip"
	versionStr := string(match[2])    // e.g. "3460100"
	versionInt, err := strconv.Atoi(versionStr)
	if err != nil {
		return Release{}, fmt.Errorf("sqlite3: parse version integer %q: %w", versionStr, err)
	}

	return Release{
		Version:     sqliteVersionIntToString(versionInt),
		VersionInt:  versionInt,
		DownloadURL: "https://www.sqlite.org/" + relPath,
	}, nil
}

// downloadSQLite3Binary downloads the tools zip from rel.DownloadURL and
// extracts the sqlite3 binary to destPath.
func downloadSQLite3Binary(ctx context.Context, rel Release, destPath string) error {
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

	return extractBinaryFromZip(tmpZip, "sqlite3", destPath)
}

// installedSQLite3Version runs {binPath} --version and returns the version
// string (e.g. "3.46.1"). Returns "" if the binary is absent or fails to run.
func installedSQLite3Version(ctx context.Context, binPath string) string {
	if _, err := os.Stat(binPath); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	// Output: "3.46.1 2024-08-13 09:16:08 ..."
	// We only want the first space-delimited token.
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// sqliteVersionIntToString converts a SQLite version integer (e.g. 3460100)
// to a dotted string (e.g. "3.46.1").
//
// SQLite encoding: MAJOR*1_000_000 + MINOR*10_000 + PATCH*100 + BUILD
func sqliteVersionIntToString(v int) string {
	major := v / 1_000_000
	minor := (v % 1_000_000) / 10_000
	patch := (v % 10_000) / 100
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// extractBinaryFromZip finds a file named `name` (or `name.exe`) inside the
// zip at zipPath and writes it to destPath atomically.
func extractBinaryFromZip(zipPath, name, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base != name && base != name+".exe" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		defer rc.Close()

		tmp := destPath + ".extract"
		out, err := os.Create(tmp)
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(tmp) // no-op after successful rename

		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return fmt.Errorf("extract %s: %w", name, err)
		}
		if err := out.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, destPath)
	}

	return fmt.Errorf("binary %q not found in %s", name, filepath.Base(zipPath))
}
