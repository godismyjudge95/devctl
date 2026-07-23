package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// pg_clickhouse is installed by extracting a prebuilt .deb into the Percona
// tree (same pattern as TimescaleDB) — no APT, no compile.
//
// Upstream ships source releases plus rolling architecture debs under the
// customer-testdeb / dev GitHub tags. We pin a known-good pg18 package.
//
// Assets: https://github.com/ClickHouse/pg_clickhouse/releases
const (
	// Package version label (deb filename / status display).
	pgClickhouseVersion = "0.2.0"
	// Extension SQL version in the control file (major.minor).
	pgClickhouseExtVersion = "0.2"
	// GitHub release tag that hosts the multi-arch debs.
	pgClickhouseDebTag = "customer-testdeb"
)

// pgClickhouseExtension implements PostgresExtension for ClickHouse FDW.
type pgClickhouseExtension struct{}

func (pgClickhouseExtension) ID() string             { return "pg_clickhouse" }
func (pgClickhouseExtension) Label() string          { return "pg_clickhouse" }
func (pgClickhouseExtension) PreloadLibrary() string { return "" } // not required
func (pgClickhouseExtension) RequiresPeer() string   { return "clickhouse" }

func (pgClickhouseExtension) IsFilesInstalled(pgDir string) bool {
	return isPgClickhouseInstalled(pgDir)
}

func isPgClickhouseInstalled(pgDir string) bool {
	return fileExists(filepath.Join(pgDir, "lib", "pg_clickhouse.so")) &&
		fileExists(filepath.Join(pgDir, "share", "extension", "pg_clickhouse.control"))
}

func (pgClickhouseExtension) FilesVersion(pgDir string) string {
	if v := parseControlDefaultVersion(filepath.Join(pgDir, "share", "extension", "pg_clickhouse.control")); v != "" {
		return v
	}
	if isPgClickhouseInstalled(pgDir) {
		return pgClickhouseVersion
	}
	return ""
}

func (e pgClickhouseExtension) InstallFiles(ctx context.Context, w io.Writer, pgDir string) error {
	if isPgClickhouseInstalled(pgDir) {
		fmt.Fprintln(w, "postgres: pg_clickhouse already installed")
		return nil
	}
	return installPgClickhouse(ctx, w, pgDir)
}

func (e pgClickhouseExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	// Re-extract when control version is missing or older than pinned.
	cur := e.FilesVersion(pgDir)
	if isPgClickhouseInstalled(pgDir) && (cur == pgClickhouseExtVersion || cur == pgClickhouseVersion) {
		fmt.Fprintln(w, "postgres: pg_clickhouse already at target version")
		return nil
	}
	return installPgClickhouse(ctx, w, pgDir)
}

func (pgClickhouseExtension) IsWired(env ExtensionEnv) bool {
	if !env.ClickHouseInstalled() || !pgIsReady(env.PGDir()) {
		return false
	}
	out, err := runPSQLQuery(context.Background(), env, "template1",
		`SELECT 1 FROM pg_extension WHERE extname = 'pg_clickhouse'`)
	if err != nil || out != "1" {
		return false
	}
	out, err = runPSQLQuery(context.Background(), env, "template1",
		`SELECT 1 FROM pg_foreign_server WHERE srvname = 'clickhouse'`)
	return err == nil && out == "1"
}

func (pgClickhouseExtension) Wire(ctx context.Context, env ExtensionEnv) (bool, error) {
	if !isPgClickhouseInstalled(env.PGDir()) {
		return true, nil
	}
	if !env.ClickHouseInstalled() {
		return true, nil
	}
	if !fileExists(filepath.Join(env.PGDir(), "data", "PG_VERSION")) {
		return true, nil
	}
	if !pgIsReady(env.PGDir()) {
		return false, nil
	}

	// Wire only template1 so new databases inherit the extension + FDW server.
	// Avoid DO $$ blocks — runPSQL passes SQL through a shell-quoted -c and $ expands.
	if err := runPSQL(ctx, env, "template1", `CREATE EXTENSION IF NOT EXISTS pg_clickhouse`); err != nil {
		return true, fmt.Errorf("postgres: wire pg_clickhouse on template1: %w", err)
	}

	exists, err := runPSQLQuery(ctx, env, "template1",
		`SELECT 1 FROM pg_foreign_server WHERE srvname = 'clickhouse'`)
	if err != nil {
		return true, fmt.Errorf("postgres: check foreign server: %w", err)
	}
	if exists != "1" {
		sql := `CREATE SERVER clickhouse FOREIGN DATA WRAPPER clickhouse_fdw OPTIONS (driver 'http', host '127.0.0.1', port '8123', dbname 'default')`
		if err := runPSQL(ctx, env, "template1", sql); err != nil {
			return true, fmt.Errorf("postgres: CREATE SERVER clickhouse: %w", err)
		}
	}

	mapSQL := fmt.Sprintf(
		`CREATE USER MAPPING IF NOT EXISTS FOR %s SERVER clickhouse OPTIONS (user 'default', password '')`,
		quoteIdent(postgresSuperuser),
	)
	if err := runPSQL(ctx, env, "template1", mapSQL); err != nil {
		return true, fmt.Errorf("postgres: CREATE USER MAPPING: %w", err)
	}
	return true, nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func pgClickhouseDebArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

func pgClickhouseDebName() string {
	return fmt.Sprintf("pg-clickhouse-%s-pg%s-%s.deb",
		pgClickhouseVersion, postgresMajor, pgClickhouseDebArch())
}

func pgClickhouseDebURL() string {
	return fmt.Sprintf(
		"https://github.com/ClickHouse/pg_clickhouse/releases/download/%s/%s",
		pgClickhouseDebTag, pgClickhouseDebName(),
	)
}

// installPgClickhouse downloads and extracts the prebuilt deb into the Percona tree.
func installPgClickhouse(ctx context.Context, w io.Writer, pgDir string) error {
	libDir := filepath.Join(pgDir, "lib")
	extDir := filepath.Join(pgDir, "share", "extension")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("postgres: pg_clickhouse lib dir: %w", err)
	}
	if err := os.MkdirAll(extDir, 0755); err != nil {
		return fmt.Errorf("postgres: pg_clickhouse extension dir: %w", err)
	}

	name := pgClickhouseDebName()
	tmpDeb := filepath.Join(os.TempDir(), name)
	// Basename of DEST must match scripts/download-artifacts.sh curl-shim cache.
	fmt.Fprintf(w, "postgres: downloading pg_clickhouse %s...\n", pgClickhouseVersion)
	if err := curlDownloadW(ctx, w, pgClickhouseDebURL(), tmpDeb); err != nil {
		return fmt.Errorf("postgres: download pg_clickhouse: %w", err)
	}
	defer os.Remove(tmpDeb)

	fmt.Fprintln(w, "postgres: extracting pg_clickhouse...")
	if err := extractPgExtensionDeb(tmpDeb, libDir, extDir); err != nil {
		return fmt.Errorf("postgres: extract pg_clickhouse: %w", err)
	}

	if !isPgClickhouseInstalled(pgDir) {
		return fmt.Errorf("postgres: pg_clickhouse extract finished but files missing under %s", pgDir)
	}
	fmt.Fprintf(w, "postgres: pg_clickhouse %s installed\n", pgClickhouseVersion)
	return nil
}

// extractPgExtensionDeb unpacks a PostgreSQL extension .deb and copies:
//   - usr/lib/postgresql/<major>/lib/*          → libDir (including bitcode/)
//   - usr/share/postgresql/<major>/extension/*  → extDir
//
// Shared with Timescale-style packages built against PGDG paths.
func extractPgExtensionDeb(debPath, libDir, extDir string) error {
	tmpDir, err := os.MkdirTemp("", "pgext-deb-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	if out, err := runShell(ctx, fmt.Sprintf("dpkg-deb --extract %s %s", shellQuote(debPath), shellQuote(tmpDir))); err != nil {
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

	// Copy lib tree (files + bitcode subdirs).
	if err := copyTree(srcLib, libDir); err != nil {
		return fmt.Errorf("copy lib: %w", err)
	}
	if err := copyTree(srcExt, extDir); err != nil {
		return fmt.Errorf("copy extension: %w", err)
	}
	return nil
}

// copyTree copies files and directories from src into dst (merge).
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Source may be missing (e.g. empty package).
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}
