package install

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// contribSearchExtension implements PostgresExtension for a stock PostgreSQL
// contrib module already extracted into the Percona tree. Files ship with the
// base build. One value per registry row.
type contribSearchExtension struct {
	id    string // extension name == control filename stem == .so stem
	label string
}

func bundledSearchContribExtensions() []PostgresExtension {
	return []PostgresExtension{
		contribSearchExtension{id: "pg_trgm", label: "pg_trgm"},
		contribSearchExtension{id: "unaccent", label: "unaccent"},
		contribSearchExtension{id: "fuzzystrmatch", label: "fuzzystrmatch"},
		contribSearchExtension{id: "btree_gin", label: "btree_gin"},
	}
}

func (e contribSearchExtension) ID() string             { return e.id }
func (e contribSearchExtension) Label() string          { return e.label }
func (e contribSearchExtension) PreloadLibrary() string { return "" }
func (e contribSearchExtension) RequiresPeer() string   { return "" }

func (e contribSearchExtension) IsFilesInstalled(pgDir string) bool {
	return fileExists(filepath.Join(pgDir, "lib", e.id+".so")) &&
		fileExists(filepath.Join(pgDir, "share", "extension", e.id+".control"))
}

func (e contribSearchExtension) InstallFiles(_ context.Context, w io.Writer, pgDir string) error {
	if e.IsFilesInstalled(pgDir) {
		fmt.Fprintf(w, "postgres: %s already present in Percona tree\n", e.id)
		return nil
	}
	return fmt.Errorf("postgres: %s files missing under %s (expected lib/%s.so and share/extension/%s.control)", e.id, pgDir, e.id, e.id)
}

func (e contribSearchExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return e.InstallFiles(ctx, w, pgDir)
}

func (e contribSearchExtension) FilesVersion(pgDir string) string {
	return parseControlDefaultVersion(filepath.Join(pgDir, "share", "extension", e.id+".control"))
}

func (e contribSearchExtension) IsWired(env ExtensionEnv) bool {
	if !pgIsReady(env.PGDir()) {
		return false
	}
	dbs, err := listConnectableDatabases(context.Background(), env)
	if err != nil || len(dbs) == 0 {
		return false
	}
	sql := contribWiredSQL(e.id)
	for _, db := range dbs {
		out, err := runPSQLQuery(context.Background(), env, db, sql)
		if err != nil || out != "1" {
			return false
		}
	}
	return true
}

func (e contribSearchExtension) Wire(ctx context.Context, env ExtensionEnv) (bool, error) {
	if !e.IsFilesInstalled(env.PGDir()) {
		return true, nil
	}
	if !fileExists(filepath.Join(env.PGDir(), "data", "PG_VERSION")) {
		return true, nil
	}
	if !pgIsReady(env.PGDir()) {
		return false, nil
	}
	dbs, err := listConnectableDatabases(ctx, env)
	if err != nil {
		if !pgIsReady(env.PGDir()) {
			return false, nil
		}
		return true, fmt.Errorf("postgres: list databases for %s: %w", e.id, err)
	}
	if len(dbs) == 0 {
		return false, nil
	}
	sql := contribCreateSQL(e.id)
	for _, db := range dbs {
		if err := runPSQL(ctx, env, db, sql); err != nil {
			return true, fmt.Errorf("postgres: CREATE EXTENSION %s on %s: %w", e.id, db, err)
		}
	}
	return true, nil
}

// contribCreateSQL must not contain $. runPSQL passes -c through a shell and $ expands.
func contribCreateSQL(id string) string {
	return "CREATE EXTENSION IF NOT EXISTS " + quoteIdent(id)
}

func contribWiredSQL(id string) string {
	return "SELECT 1 FROM pg_extension WHERE extname = '" + strings.ReplaceAll(id, "'", "''") + "'"
}

const contribListDatabasesSQL = `SELECT datname FROM pg_database WHERE datallowconn AND datname <> 'template0'`

func listConnectableDatabases(ctx context.Context, env ExtensionEnv) ([]string, error) {
	out, err := runPSQLQuery(ctx, env, "postgres", contribListDatabasesSQL)
	if err != nil {
		return nil, err
	}
	return parseConnectableDatabases(out), nil
}

// parseConnectableDatabases puts template1 first so Wire creates it there
// before other databases (a CREATE DATABASE during this pass then inherits).
func parseConnectableDatabases(out string) []string {
	var rest []string
	hasTemplate1 := false
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || name == "template0" {
			continue
		}
		if name == "template1" {
			hasTemplate1 = true
			continue
		}
		rest = append(rest, name)
	}
	if hasTemplate1 {
		return append([]string{"template1"}, rest...)
	}
	return rest
}
