package install

import (
	"context"
	"io"
)

// Percona ships pgvector built with -march=native on AVX-512 builders. That
// binary SIGILLs on CPUs without AVX-512 (e.g. Zen 2 under WSL2). The devctl
// pg-extensions bundle replaces it with a build made with OPTFLAGS="".
//
// Upstream: https://github.com/pgvector/pgvector
const pgvectorVersion = "0.8.2"

// pgvectorExtension replaces the vendor vector library with the portable build.
type pgvectorExtension struct{}

func (pgvectorExtension) ID() string             { return "pgvector" }
func (pgvectorExtension) Label() string          { return "pgvector" }
func (pgvectorExtension) PreloadLibrary() string { return "" }
func (pgvectorExtension) RequiresPeer() string   { return "" }

func (pgvectorExtension) IsFilesInstalled(pgDir string) bool {
	return isPgvectorPortable(pgDir)
}

func (pgvectorExtension) FilesVersion(pgDir string) string {
	if v := parseControlDefaultVersion(pgExtensionControl(pgDir, "vector")); v != "" {
		return v
	}
	if isPgvectorPortable(pgDir) {
		return pgvectorVersion
	}
	return ""
}

func (e pgvectorExtension) InstallFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return installPgExtensionsBundle(ctx, w, pgDir)
}

func (e pgvectorExtension) UpdateFiles(ctx context.Context, w io.Writer, pgDir string) error {
	return installPgExtensionsBundle(ctx, w, pgDir)
}

// No SQL wiring — apps CREATE EXTENSION vector in their own databases.
func (pgvectorExtension) IsWired(env ExtensionEnv) bool {
	return isPgvectorPortable(env.PGDir())
}

func (pgvectorExtension) Wire(context.Context, ExtensionEnv) (bool, error) {
	return true, nil
}

func isPgvectorPortable(pgDir string) bool {
	return pgExtensionsBundleCurrent(pgDir) &&
		pgExtensionLibrary(pgDir, "vector") != "" &&
		pgExtensionControl(pgDir, "vector") != ""
}
