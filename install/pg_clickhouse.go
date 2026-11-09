package install

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// pg_clickhouse comes from the devctl pg-extensions bundle.
// Upstream: https://github.com/ClickHouse/pg_clickhouse
const pgClickhouseVersion = "0.11.0"

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
	return fileExists(pgShlibPath(pgDir, "pg_clickhouse")) &&
		fileExists(pgExtensionControl(pgDir, "pg_clickhouse"))
}

func (pgClickhouseExtension) FilesVersion(pgDir string) string {
	if v := parseControlDefaultVersion(pgExtensionControl(pgDir, "pg_clickhouse")); v != "" {
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
	return installPgExtensionsBundle(ctx, w, pgDir)
}

func (e pgClickhouseExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return installPgExtensionsBundle(ctx, w, pgDir)
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
	// Installs from the old Linux package have SQL objects at 0.2.
	if err := runPSQL(ctx, env, "template1", `ALTER EXTENSION pg_clickhouse UPDATE`); err != nil {
		return true, fmt.Errorf("postgres: update pg_clickhouse on template1: %w", err)
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

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}
