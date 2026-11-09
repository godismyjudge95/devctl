package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// TimescaleDB, pgvector, and pg_clickhouse come prebuilt from one devctl
// release asset per platform. .github/workflows/build-pg-extensions.yml builds
// them against the same PostgreSQL build this package installs. Users need no
// compiler, cmake, git, or Xcode Command Line Tools.
//
// Bump the tag after a new pg-extensions-* release is published.
const pgExtensionsReleaseTag = "pg-extensions-20261108.1"

func pgExtensionsPlatform() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/arm64":
		return "linux-aarch64"
	case "darwin/arm64":
		return "macos-aarch64"
	default:
		return "linux-x86_64"
	}
}

// pgExtensionsAssetName is also the download basename, so the test curl shim
// can serve it from the artifact cache.
func pgExtensionsAssetName() string {
	return fmt.Sprintf("pg-extensions-pg%s-%s.tar.gz", postgresVersion, pgExtensionsPlatform())
}

func pgExtensionsURL() string {
	return fmt.Sprintf("https://github.com/godismyjudge95/devctl/releases/download/%s/%s",
		pgExtensionsReleaseTag, pgExtensionsAssetName())
}

func pgExtensionsStampPath(pgDir string) string {
	return filepath.Join(pgLibDir(pgDir), ".devctl-pg-extensions")
}

func pgExtensionsStamp() string {
	return pgExtensionsReleaseTag + "/" + pgExtensionsAssetName()
}

// pgExtensionsBundleCurrent reports whether the pinned bundle was unpacked
// into pgDir. A PostgreSQL update removes the stamp because the vendor
// archive overwrites vector.so.
func pgExtensionsBundleCurrent(pgDir string) bool {
	data, err := os.ReadFile(pgExtensionsStampPath(pgDir))
	return err == nil && strings.TrimSpace(string(data)) == pgExtensionsStamp()
}

func installPgExtensionsBundle(ctx context.Context, w io.Writer, pgDir string) error {
	if pgExtensionsBundleCurrent(pgDir) {
		return nil
	}
	tmp := filepath.Join(os.TempDir(), pgExtensionsAssetName())
	defer os.Remove(tmp)
	fmt.Fprintf(w, "postgres: downloading extensions (%s)...\n", pgExtensionsReleaseTag)
	if err := curlDownloadW(ctx, w, pgExtensionsURL(), tmp); err != nil {
		return fmt.Errorf("postgres: download extensions: %w", err)
	}
	if err := extractPgExtensionsBundle(tmp, pgLibDir(pgDir), pgShareExtDir(pgDir)); err != nil {
		return fmt.Errorf("postgres: extract extensions: %w", err)
	}
	if err := os.WriteFile(pgExtensionsStampPath(pgDir), []byte(pgExtensionsStamp()+"\n"), 0644); err != nil {
		return fmt.Errorf("postgres: extensions stamp: %w", err)
	}
	fmt.Fprintln(w, "postgres: TimescaleDB, pgvector, and pg_clickhouse installed")
	return nil
}

// extractPgExtensionsBundle writes lib/* into libDir and extension/* into
// extDir. Each file is written beside its target and renamed over it, so a
// running PostgreSQL keeps the library it already mapped.
func extractPgExtensionsBundle(archive, libDir, extDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	roots := map[string]string{"lib": libDir, "extension": extDir}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		top, rel, _ := strings.Cut(name, "/")
		root, ok := roots[top]
		if !ok || rel == "" {
			continue
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("unsafe path %q", hdr.Name)
		}
		dest := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.Remove(dest)
			if err := os.Symlink(hdr.Linkname, dest); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := replaceFile(dest, tr, os.FileMode(hdr.Mode).Perm()); err != nil {
				return err
			}
		}
	}
}

func replaceFile(dest string, r io.Reader, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
