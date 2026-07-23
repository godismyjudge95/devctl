package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danielgormly/devctl/internal/runuser"
)

// TimescaleDB Community Edition is shipped as PostgreSQL extension files
// extracted from packagecloud .deb packages — no APT, no install scripts.
// Community (TSL) includes compression, continuous aggregates, and other
// features not present in the Apache 2 OSS builds.
//
// Package source: https://packagecloud.io/timescale/timescaledb
// We pin jammy (ubuntu22.04) builds; they run on newer Ubuntu with libc6 ≥ 2.17.
// The -1804 build tag means the packages were compiled against PostgreSQL 18.4;
// postgresVersion must be ≥ 18.4.
const (
	timescaledbVersion = "2.28.3"
	// timescaledbPkgTag is the packagecloud version suffix: ~ubuntu22.04-1804
	timescaledbPkgTag = "2.28.3~ubuntu22.04-1804"
	// timescaledbDist is the packagecloud dist path segment under ubuntu/pool/.
	timescaledbDist = "jammy"
)

// isTimescaleInstalled reports whether the Community Edition files are present
// (loader + versioned core .so + TSL .so + control file).
func isTimescaleInstalled(pgDir string) bool {
	lib := filepath.Join(pgDir, "lib")
	return fileExists(filepath.Join(lib, "timescaledb.so")) &&
		fileExists(filepath.Join(pgDir, "share", "extension", "timescaledb.control")) &&
		fileExists(filepath.Join(lib, "timescaledb-"+timescaledbVersion+".so")) &&
		fileExists(filepath.Join(lib, "timescaledb-tsl-"+timescaledbVersion+".so"))
}

// timescaleDebArch maps GOARCH to Debian package arch.
func timescaleDebArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// timescaleLoaderDebName is the loader package basename (curl-shim cache key).
func timescaleLoaderDebName() string {
	return fmt.Sprintf("timescaledb-2-loader-postgresql-%s_%s_%s.deb",
		postgresMajor, timescaledbPkgTag, timescaleDebArch())
}

// timescaleCommunityDebName is the Community Edition package basename.
func timescaleCommunityDebName() string {
	return fmt.Sprintf("timescaledb-2-%s-postgresql-%s_%s_%s.deb",
		timescaledbVersion, postgresMajor, timescaledbPkgTag, timescaleDebArch())
}

// timescaleLoaderDebURL returns the packagecloud pool URL for the loader deb.
// Provides timescaledb.so + timescaledb.control.
func timescaleLoaderDebURL() string {
	return fmt.Sprintf(
		"https://packagecloud.io/timescale/timescaledb/ubuntu/pool/%s/main/t/timescaledb-2-%s-postgresql-%s/%s",
		timescaledbDist, timescaledbVersion, postgresMajor, timescaleLoaderDebName(),
	)
}

// timescaleCommunityDebURL returns the packagecloud pool URL for the Community
// Edition extension deb (single-version package — not the multi-version meta package).
// Includes timescaledb-<ver>.so and timescaledb-tsl-<ver>.so.
func timescaleCommunityDebURL() string {
	return fmt.Sprintf(
		"https://packagecloud.io/timescale/timescaledb/ubuntu/pool/%s/main/t/timescaledb-2-%s-postgresql-%s/%s",
		timescaledbDist, timescaledbVersion, postgresMajor, timescaleCommunityDebName(),
	)
}

func timescaleDebs() []struct {
	url  string
	name string
	desc string
} {
	return []struct {
		url  string
		name string
		desc string
	}{
		{timescaleLoaderDebURL(), timescaleLoaderDebName(), "TimescaleDB loader"},
		{timescaleCommunityDebURL(), timescaleCommunityDebName(), "TimescaleDB Community"},
	}
}

// installTimescale downloads and extracts TimescaleDB Community Edition into
// the Percona PostgreSQL tree, then enables shared_preload_libraries.
func installTimescale(ctx context.Context, w io.Writer, pgDir string) error {
	if isTimescaleInstalled(pgDir) {
		fmt.Fprintln(w, "postgres: TimescaleDB already installed")
		if err := ensureTimescalePreload(pgDir); err != nil {
			return err
		}
		return nil
	}

	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("postgres: timescale lib dir: %w", err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		return fmt.Errorf("postgres: timescale extension dir: %w", err)
	}

	for _, d := range timescaleDebs() {
		tmpDeb := filepath.Join(os.TempDir(), d.name)
		// Basename of DEST must match scripts/download-artifacts.sh so the
		// test curl shim can serve cached packages.
		fmt.Fprintf(w, "postgres: downloading %s...\n", d.desc)
		if err := curlDownloadW(ctx, w, d.url, tmpDeb); err != nil {
			os.Remove(tmpDeb)
			return fmt.Errorf("postgres: download %s: %w", d.desc, err)
		}
		fmt.Fprintf(w, "postgres: extracting %s...\n", d.desc)
		if err := extractTimescaleDeb(tmpDeb, libDir, extDir); err != nil {
			os.Remove(tmpDeb)
			return fmt.Errorf("postgres: extract %s: %w", d.desc, err)
		}
		os.Remove(tmpDeb)
	}

	if err := ensureTimescalePreload(pgDir); err != nil {
		return err
	}

	fmt.Fprintf(w, "postgres: TimescaleDB %s (Community Edition) installed\n", timescaledbVersion)
	return nil
}

// updateTimescale re-downloads and extracts TimescaleDB Community Edition over
// an existing Postgres install. Safe when Timescale was not previously installed
// (including upgrades from Apache OSS builds that lack the TSL library).
func updateTimescale(ctx context.Context, w io.Writer, pgDir string) error {
	// Force re-extract of versioned libraries (core + TSL).
	libDir := filepath.Join(pgDir, "lib")
	_ = os.Remove(filepath.Join(libDir, "timescaledb-"+timescaledbVersion+".so"))
	_ = os.Remove(filepath.Join(libDir, "timescaledb-tsl-"+timescaledbVersion+".so"))

	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("postgres: timescale lib dir: %w", err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		return fmt.Errorf("postgres: timescale extension dir: %w", err)
	}

	for _, d := range timescaleDebs() {
		tmpDeb := filepath.Join(os.TempDir(), d.name)
		fmt.Fprintf(w, "postgres: downloading %s...\n", d.desc)
		if err := curlDownloadW(ctx, w, d.url, tmpDeb); err != nil {
			os.Remove(tmpDeb)
			return fmt.Errorf("postgres: update download %s: %w", d.desc, err)
		}
		fmt.Fprintf(w, "postgres: extracting %s...\n", d.desc)
		if err := extractTimescaleDeb(tmpDeb, libDir, extDir); err != nil {
			os.Remove(tmpDeb)
			return fmt.Errorf("postgres: update extract %s: %w", d.desc, err)
		}
		os.Remove(tmpDeb)
	}

	if err := ensureTimescalePreload(pgDir); err != nil {
		return err
	}

	fmt.Fprintf(w, "postgres: TimescaleDB updated to %s (Community Edition)\n", timescaledbVersion)
	return nil
}

// extractTimescaleDeb unpacks a TimescaleDB .deb and copies:
//   - usr/lib/postgresql/<major>/lib/*          → libDir
//   - usr/share/postgresql/<major>/extension/*  → extDir
func extractTimescaleDeb(debPath, libDir, extDir string) error {
	tmpDir, err := os.MkdirTemp("", "timescale-deb-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	if out, err := runShell(ctx, fmt.Sprintf("dpkg-deb --extract %s %s", debPath, tmpDir)); err != nil {
		return fmt.Errorf("dpkg-deb extract: %w\n%s", err, out)
	}

	srcLib := filepath.Join(tmpDir, "usr", "lib", "postgresql", postgresMajor, "lib")
	srcExt := filepath.Join(tmpDir, "usr", "share", "postgresql", postgresMajor, "extension")

	if err := os.MkdirAll(libDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		return err
	}

	if entries, err := os.ReadDir(srcLib); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(srcLib, e.Name())
			dst := filepath.Join(libDir, e.Name())
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copy %s: %w", e.Name(), err)
			}
		}
	}

	if entries, err := os.ReadDir(srcExt); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(srcExt, e.Name())
			dst := filepath.Join(extDir, e.Name())
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copy %s: %w", e.Name(), err)
			}
		}
	}

	return nil
}

// ensureTimescalePreload sets shared_preload_libraries to include timescaledb
// in data/postgresql.conf (creates/merges the setting without duplicating).
func ensureTimescalePreload(pgDir string) error {
	confPath := filepath.Join(pgDir, "data", "postgresql.conf")
	if !fileExists(confPath) {
		return nil // initdb not done yet — caller writes conf after
	}
	return ensureSharedPreloadLibraries(confPath, "timescaledb")
}

// ensureSharedPreloadLibraries ensures name appears in shared_preload_libraries.
// If the setting is missing, appends it. If present, merges name into the list.
func ensureSharedPreloadLibraries(confPath, name string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return fmt.Errorf("read postgresql.conf: %w", err)
	}

	newData, changed := mergeSharedPreloadLibraries(string(data), name)
	if !changed {
		return nil
	}
	return os.WriteFile(confPath, []byte(newData), 0600)
}

// mergeSharedPreloadLibraries returns the conf text with name included in
// shared_preload_libraries. changed is false when no write is needed.
func mergeSharedPreloadLibraries(conf, name string) (string, bool) {
	lines := strings.Split(conf, "\n")
	found := false
	changed := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Match active shared_preload_libraries = ...
		key, val, ok := splitConfAssignment(trimmed)
		if !ok || key != "shared_preload_libraries" {
			continue
		}
		found = true
		libs := parsePreloadList(val)
		if containsFold(libs, name) {
			return conf, false
		}
		libs = append(libs, name)
		// Preserve original indentation of the line.
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = indent + "shared_preload_libraries = '" + strings.Join(libs, ",") + "'"
		changed = true
		break
	}

	if !found {
		// Append under a small marker block.
		extra := "\n# devctl: TimescaleDB (Community Edition)\nshared_preload_libraries = '" + name + "'\n"
		if !strings.HasSuffix(conf, "\n") {
			extra = "\n" + extra
		}
		return conf + extra, true
	}
	if !changed {
		return conf, false
	}
	return strings.Join(lines, "\n"), true
}

// splitConfAssignment parses `key = value` (value may be quoted).
func splitConfAssignment(line string) (key, val string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	// Strip trailing comment.
	if i := strings.IndexByte(val, '#'); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	val = strings.Trim(val, "'\"")
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func parsePreloadList(val string) []string {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "'\"")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsFold(list []string, name string) bool {
	for _, s := range list {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// ensureTimescaleExtension creates the timescaledb extension in the default
// postgres database when the server is up and the files are installed.
// Returns (false, nil) when the server is not ready yet (caller may retry).
// Safe to call repeatedly. No-op when files are missing.
func ensureTimescaleExtension(pgDir, siteUser, siteHome string) (ready bool, err error) {
	if !isTimescaleInstalled(pgDir) {
		return true, nil
	}
	dataDir := filepath.Join(pgDir, "data")
	if !fileExists(filepath.Join(dataDir, "PG_VERSION")) {
		return true, nil
	}

	// Socket lives in /tmp by default for the Percona tarball — use TCP + password.
	readyCmd := fmt.Sprintf("LD_LIBRARY_PATH=%s/lib %s/bin/pg_isready -h 127.0.0.1 -p 5432 -q", pgDir, pgDir)
	if _, err := runShell(context.Background(), readyCmd); err != nil {
		return false, nil // server not up — caller may retry
	}

	sql := `CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;`
	cmd := fmt.Sprintf(
		`PGPASSWORD=%q LD_LIBRARY_PATH=%q/lib %q/bin/psql.bin -h 127.0.0.1 -p 5432 -U %q -d postgres -v ON_ERROR_STOP=1 -c %q`,
		postgresDevPassword, pgDir, pgDir, postgresSuperuser, sql,
	)
	if out, err := runuser.RunAsUserW(context.Background(), io.Discard, siteUser, siteHome, "", cmd); err != nil {
		return true, fmt.Errorf("postgres: CREATE EXTENSION timescaledb: %w\n%s", err, out)
	}
	return true, nil
}
