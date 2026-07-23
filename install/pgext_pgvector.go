package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Percona ships pgvector built with -march=native on AVX-512 builders. That
// binary SIGILLs on CPUs without AVX-512 (e.g. Zen 2 under WSL2). Rebuild from
// upstream source with OPTFLAGS="" so AVX-512 stays in CPU-dispatch paths only.
//
// Upstream: https://github.com/pgvector/pgvector
const (
	pgvectorVersion = "0.8.2"
	pgvectorTag     = "v" + pgvectorVersion
)

// pgvectorExtension rebuilds the bundled vector.so as a portable binary.
type pgvectorExtension struct{}

func (pgvectorExtension) ID() string             { return "pgvector" }
func (pgvectorExtension) Label() string          { return "pgvector" }
func (pgvectorExtension) PreloadLibrary() string { return "" }
func (pgvectorExtension) RequiresPeer() string   { return "" }

func (pgvectorExtension) IsFilesInstalled(pgDir string) bool {
	return isPgvectorPortable(pgDir)
}

func (pgvectorExtension) FilesVersion(pgDir string) string {
	if v := parseControlDefaultVersion(filepath.Join(pgDir, "share", "extension", "vector.control")); v != "" {
		return v
	}
	if isPgvectorPortable(pgDir) {
		return pgvectorVersion
	}
	return ""
}

func (e pgvectorExtension) InstallFiles(ctx context.Context, w io.Writer, pgDir string) error {
	if isPgvectorPortable(pgDir) {
		fmt.Fprintln(w, "postgres: pgvector portable build already installed")
		return nil
	}
	return installPgvectorPortable(ctx, w, pgDir)
}

func (e pgvectorExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	// Always rebuild: a Percona tarball extract overwrites vector.so with the
	// unsafe -march=native binary while leaving our stamp file behind.
	return installPgvectorPortable(ctx, w, pgDir)
}

// No SQL wiring — apps CREATE EXTENSION vector in their own databases.
func (pgvectorExtension) IsWired(env ExtensionEnv) bool {
	return isPgvectorPortable(env.PGDir())
}

func (pgvectorExtension) Wire(context.Context, ExtensionEnv) (bool, error) {
	return true, nil
}

func pgvectorPortableStampPath(pgDir string) string {
	return filepath.Join(pgDir, "lib", ".devctl-pgvector-portable")
}

func isPgvectorPortable(pgDir string) bool {
	if !fileExists(filepath.Join(pgDir, "lib", "vector.so")) {
		return false
	}
	if !fileExists(filepath.Join(pgDir, "share", "extension", "vector.control")) {
		return false
	}
	data, err := os.ReadFile(pgvectorPortableStampPath(pgDir))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == pgvectorVersion
}

func writePgvectorPortableStamp(pgDir string) error {
	return os.WriteFile(pgvectorPortableStampPath(pgDir), []byte(pgvectorVersion+"\n"), 0644)
}

func pgvectorSourceURL() string {
	return fmt.Sprintf("https://github.com/pgvector/pgvector/archive/refs/tags/%s.tar.gz", pgvectorTag)
}

func installPgvectorPortable(ctx context.Context, w io.Writer, pgDir string) error {
	pgConfig := filepath.Join(pgDir, "bin", "pg_config")
	if !fileExists(pgConfig) {
		return fmt.Errorf("postgres: pgvector: pg_config not found at %s", pgConfig)
	}
	if err := ensurePgvectorBuildTools(ctx, w); err != nil {
		return err
	}

	fmt.Fprintf(w, "postgres: rebuilding pgvector %s with OPTFLAGS=\"\" (portable)...\n", pgvectorVersion)

	tmpDir, err := os.MkdirTemp("", "pgvector-src-*")
	if err != nil {
		return fmt.Errorf("postgres: pgvector temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, "pgvector.tar.gz")
	if err := curlDownloadW(ctx, w, pgvectorSourceURL(), tarball); err != nil {
		return fmt.Errorf("postgres: pgvector download: %w", err)
	}

	srcRoot := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcRoot, 0755); err != nil {
		return fmt.Errorf("postgres: pgvector mkdir: %w", err)
	}
	extractCmd := fmt.Sprintf("tar -xzf %s -C %s --strip-components=1", shellQuote(tarball), shellQuote(srcRoot))
	if out, err := runShellW(ctx, w, extractCmd); err != nil {
		return fmt.Errorf("postgres: pgvector extract: %w\n%s", err, out)
	}

	// OPTFLAGS="" must be passed to both make and make install (upstream docs).
	// PG_CONFIG must also be set on `make clean` — the Makefile evaluates
	// $(shell $(PG_CONFIG) ...) at parse time, and pg_config is not on PATH.
	buildCmd := fmt.Sprintf(
		`make clean PG_CONFIG=%s && make PG_CONFIG=%s OPTFLAGS="" && make install PG_CONFIG=%s OPTFLAGS=""`,
		shellQuote(pgConfig), shellQuote(pgConfig), shellQuote(pgConfig),
	)
	if out, err := runShellInDirW(ctx, w, srcRoot, buildCmd); err != nil {
		return fmt.Errorf("postgres: pgvector build: %w\n%s", err, out)
	}

	if err := writePgvectorPortableStamp(pgDir); err != nil {
		return fmt.Errorf("postgres: pgvector stamp: %w", err)
	}
	fmt.Fprintln(w, "postgres: pgvector portable build installed")
	return nil
}

func ensurePgvectorBuildTools(ctx context.Context, w io.Writer) error {
	if _, err := exec.LookPath("gcc"); err == nil {
		if _, err := exec.LookPath("make"); err == nil {
			return nil
		}
	}
	fmt.Fprintln(w, "postgres: installing build-essential for pgvector...")
	if err := aptInstallW(ctx, w, "build-essential"); err != nil {
		return fmt.Errorf("postgres: pgvector needs gcc/make: %w", err)
	}
	return nil
}
