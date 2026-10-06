package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const timescaledbSourceURL = "https://github.com/timescale/timescaledb/archive/refs/tags/" + timescaledbVersion + ".tar.gz"

func cmakeBin() string {
	if p, err := exec.LookPath("cmake"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/cmake", "/usr/local/bin/cmake"} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// installTimescaleFromSource builds TimescaleDB Community Edition against the
// nested PostgreSQL tree. Darwin has no vendor .deb. cmake is used if present
// on PATH; this does not invoke Homebrew.
func installTimescaleFromSource(ctx context.Context, w io.Writer, pgDir string) error {
	cmake := cmakeBin()
	if cmake == "" {
		return fmt.Errorf("postgres: timescale on darwin needs cmake on PATH (binary download of cmake, not brew)")
	}
	pgConfig := filepath.Join(pgDir, "bin", "pg_config")
	if !fileExists(pgConfig) {
		return fmt.Errorf("postgres: timescale: pg_config not found at %s", pgConfig)
	}

	fmt.Fprintf(w, "postgres: building TimescaleDB %s from source (darwin)...\n", timescaledbVersion)
	tmpDir, err := os.MkdirTemp("", "timescaledb-src-*")
	if err != nil {
		return fmt.Errorf("postgres: timescale temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	shim, err := writeDarwinPgConfigShim(filepath.Join(tmpDir, "bin"), pgConfig)
	if err != nil {
		return fmt.Errorf("postgres: timescale pg_config shim: %w", err)
	}
	pgConfig = shim

	tarball := filepath.Join(tmpDir, "timescaledb.tar.gz")
	if err := curlDownloadW(ctx, w, timescaledbSourceURL, tarball); err != nil {
		return fmt.Errorf("postgres: timescale download: %w", err)
	}
	srcRoot := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcRoot, 0755); err != nil {
		return err
	}
	extractCmd := fmt.Sprintf("tar -xzf %s -C %s --strip-components=1", shellQuote(tarball), shellQuote(srcRoot))
	if out, err := runShellW(ctx, w, extractCmd); err != nil {
		return fmt.Errorf("postgres: timescale extract: %w\n%s", err, out)
	}

	envPrefix := fmt.Sprintf("PATH=%s:%s:%s:$PATH PG_CONFIG=%s %s",
		shellQuote(filepath.Dir(pgConfig)),
		shellQuote(filepath.Dir(cmake)),
		shellQuote(filepath.Join(pgDir, "bin")),
		shellQuote(pgConfig),
		darwinPGCompileVars(),
	)
	boot := fmt.Sprintf(
		`%s ./bootstrap -DCMAKE_BUILD_TYPE=Release -DREGRESS_CHECKS=OFF -DTAP_CHECKS=OFF -DWARNINGS_AS_ERRORS=OFF -DLINTER=OFF -DOPENSSL_ROOT_DIR=%s -DOPENSSL_INCLUDE_DIR=%s -DOPENSSL_SSL_LIBRARY=%s -DOPENSSL_CRYPTO_LIBRARY=%s%s`,
		envPrefix,
		shellQuote(pgDir),
		shellQuote(filepath.Join(pgDir, "include")),
		shellQuote(filepath.Join(pgDir, "lib", "libssl.dylib")),
		shellQuote(filepath.Join(pgDir, "lib", "libcrypto.dylib")),
		darwinCMakeSysrootArgs(),
	)
	if out, err := runShellInDirW(ctx, w, srcRoot, boot); err != nil {
		return fmt.Errorf("postgres: timescale bootstrap: %w\n%s", err, out)
	}
	buildDir := filepath.Join(srcRoot, "build")
	if err := rewriteStaleDarwinSysroot(buildDir); err != nil {
		return fmt.Errorf("postgres: timescale sysroot rewrite: %w", err)
	}
	if out, err := runShellInDirW(ctx, w, buildDir, envPrefix+" make -j$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"); err != nil {
		return fmt.Errorf("postgres: timescale make: %w\n%s", err, out)
	}
	if out, err := runShellInDirW(ctx, w, buildDir, envPrefix+" make install"); err != nil {
		return fmt.Errorf("postgres: timescale make install: %w\n%s", err, out)
	}
	if err := ensureTimescalePreload(pgDir); err != nil {
		return err
	}
	if !isTimescaleInstalled(pgDir) {
		return fmt.Errorf("postgres: timescale build finished but files missing under %s", pgLibDir(pgDir))
	}
	fmt.Fprintf(w, "postgres: TimescaleDB %s (Community Edition) installed from source\n", timescaledbVersion)
	return nil
}
