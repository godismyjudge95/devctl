package install

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

// timescaleExtension adapts the existing Timescale install path into the registry.
type timescaleExtension struct{}

func (timescaleExtension) ID() string             { return "timescaledb" }
func (timescaleExtension) Label() string          { return "TimescaleDB" }
func (timescaleExtension) PreloadLibrary() string { return "timescaledb" }
func (timescaleExtension) RequiresPeer() string   { return "" }

func (timescaleExtension) IsFilesInstalled(pgDir string) bool {
	return isTimescaleInstalled(pgDir)
}

func (timescaleExtension) InstallFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return installTimescale(ctx, w, pgDir)
}

func (timescaleExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return updateTimescale(ctx, w, pgDir)
}

func (timescaleExtension) FilesVersion(pgDir string) string {
	if !isTimescaleInstalled(pgDir) {
		return ""
	}
	// Prefer control file; fall back to pinned package version constant.
	if v := parseControlDefaultVersion(filepath.Join(pgDir, "share", "extension", "timescaledb.control")); v != "" {
		return v
	}
	return timescaledbVersion
}

func (timescaleExtension) IsWired(env ExtensionEnv) bool {
	if !pgIsReady(env.PGDir()) {
		return false
	}
	// Timescale is wired into the default postgres database (historical behavior).
	out, err := runPSQLQuery(context.Background(), env, "postgres",
		`SELECT 1 FROM pg_extension WHERE extname = 'timescaledb'`)
	return err == nil && out == "1"
}

func (timescaleExtension) Wire(ctx context.Context, env ExtensionEnv) (bool, error) {
	if !isTimescaleInstalled(env.PGDir()) {
		return true, nil
	}
	if !fileExists(filepath.Join(env.PGDir(), "data", "PG_VERSION")) {
		return true, nil
	}
	if !pgIsReady(env.PGDir()) {
		return false, nil
	}
	if err := runPSQL(ctx, env, "postgres", `CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;`); err != nil {
		return true, fmt.Errorf("postgres: CREATE EXTENSION timescaledb: %w", err)
	}
	return true, nil
}
