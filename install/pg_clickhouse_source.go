package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const pgClickhouseGitURL = "https://github.com/ClickHouse/pg_clickhouse.git"
const pgClickhouseGitTag = "v0.11.0"

// installPgClickhouseFromSource builds pg_clickhouse with PGXS against the
// nested PostgreSQL tree. Darwin has no vendor .deb. vendor/ is a git submodule.
func installPgClickhouseFromSource(ctx context.Context, w io.Writer, pgDir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("postgres: pg_clickhouse on darwin needs git: %w", err)
	}
	pgConfig := filepath.Join(pgDir, "bin", "pg_config")
	if !fileExists(pgConfig) {
		return fmt.Errorf("postgres: pg_clickhouse: pg_config not found at %s", pgConfig)
	}

	fmt.Fprintf(w, "postgres: building pg_clickhouse %s from source (darwin)...\n", pgClickhouseGitTag)
	tmpDir, err := os.MkdirTemp("", "pg-clickhouse-src-*")
	if err != nil {
		return fmt.Errorf("postgres: pg_clickhouse temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	shim, err := writeDarwinPgConfigShim(filepath.Join(tmpDir, "bin"), pgConfig)
	if err != nil {
		return fmt.Errorf("postgres: pg_clickhouse pg_config shim: %w", err)
	}
	pgConfig = shim

	src := filepath.Join(tmpDir, "src")
	clone := fmt.Sprintf(
		"git clone --depth 1 --recurse-submodules --branch %s %s %s",
		shellQuote(pgClickhouseGitTag),
		shellQuote(pgClickhouseGitURL),
		shellQuote(src),
	)
	if out, err := runShellW(ctx, w, clone); err != nil {
		return fmt.Errorf("postgres: pg_clickhouse clone: %w\n%s", err, out)
	}
	if !dirExists(filepath.Join(src, "vendor", "pg-clickhouse-c")) {
		return fmt.Errorf("postgres: pg_clickhouse vendor submodule missing after clone")
	}

	pgLib := filepath.Join(pgDir, "lib")
	pgInc := filepath.Join(pgDir, "include")
	pgx := fmt.Sprintf("PATH=%s:$PATH LIBRARY_PATH=%s CPATH=%s C_INCLUDE_PATH=%s %s make PG_CONFIG=%s %s && PATH=%s:$PATH LIBRARY_PATH=%s CPATH=%s C_INCLUDE_PATH=%s %s make install PG_CONFIG=%s %s",
		shellQuote(filepath.Dir(pgConfig)), shellQuote(pgLib), shellQuote(pgInc), shellQuote(pgInc), darwinPGCompileVars(), shellQuote(pgConfig), darwinPGXSMakeArgs(),
		shellQuote(filepath.Dir(pgConfig)), shellQuote(pgLib), shellQuote(pgInc), shellQuote(pgInc), darwinPGCompileVars(), shellQuote(pgConfig), darwinPGXSMakeArgs())
	if out, err := runShellInDirW(ctx, w, src, pgx); err != nil {
		return fmt.Errorf("postgres: pg_clickhouse build: %w\n%s", err, out)
	}
	if !isPgClickhouseInstalled(pgDir) {
		return fmt.Errorf("postgres: pg_clickhouse build finished but files missing under %s", pgLibDir(pgDir))
	}
	fmt.Fprintf(w, "postgres: pg_clickhouse %s installed from source\n", pgClickhouseGitTag)
	return nil
}
