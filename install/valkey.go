package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/ulikunitz/xz"
)

const valkeyVersion = "9.0.3"

// valkeyTarURL returns the download URL for the Valkey tarball.
// Valkey ships distro-specific builds; we prefer noble (Ubuntu 24.04 / glibc
// 2.39) on noble systems and fall back to jammy (Ubuntu 22.04 / glibc 2.35)
// everywhere else — including Debian bookworm (glibc 2.36).
func valkeyTarURL(ctx context.Context) string {
	codename, _ := lsbReleaseName(ctx)
	distro := "jammy"
	if codename == "noble" {
		distro = "noble"
	}
	return fmt.Sprintf("https://download.valkey.io/releases/valkey-%s-%s-x86_64.tar.gz", valkeyVersion, distro)
}

// ValkeyInstaller downloads the Valkey binary to
// {serverRoot}/valkey/ and runs it as a supervised child process.
// Valkey is a Redis-compatible open-source fork; the service ID is kept as
// "redis" so existing Laravel .env files (REDIS_HOST, etc.) continue to work.
type ValkeyInstaller struct {
	supervisor *services.Supervisor
	serverRoot string // absolute path to the devctl server directory
}

func (v *ValkeyInstaller) ServiceID() string { return "redis" }

func (v *ValkeyInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(v.serverRoot, "valkey"), "valkey-server"))
}

func (v *ValkeyInstaller) Install(ctx context.Context) error {
	return v.InstallW(ctx, io.Discard)
}

func (v *ValkeyInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if v.IsInstalled() {
		fmt.Fprintln(w, "valkey: already installed")
		return nil
	}

	valkeyDir := paths.ServiceDir(v.serverRoot, "valkey")
	binPath := filepath.Join(valkeyDir, "valkey-server")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("valkey-%s.tar.gz", valkeyVersion))
	defer os.Remove(tmpTar)

	// 1. Create directory.
	fmt.Fprintln(w, "valkey: creating directory...")
	if err := os.MkdirAll(valkeyDir, 0755); err != nil {
		return fmt.Errorf("valkey: create dir: %w", err)
	}

	// 2. Determine the correct tarball URL for this system.
	url := valkeyTarURL(ctx)

	// 3. Download tarball.
	fmt.Fprintf(w, "valkey: downloading %s...\n", valkeyVersion)
	if err := curlDownloadW(ctx, w, url, tmpTar); err != nil {
		return fmt.Errorf("valkey: download: %w", err)
	}

	// 4. Extract valkey-server binary from tarball.
	fmt.Fprintln(w, "valkey: extracting binary...")
	if err := extractFromTar(tmpTar, "valkey-server", binPath); err != nil {
		return fmt.Errorf("valkey: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("valkey: chmod binary: %w", err)
	}

	// 5. Also extract valkey-cli if present in the tarball.
	cliBinPath := filepath.Join(valkeyDir, "valkey-cli")
	if err := extractFromTar(tmpTar, "valkey-cli", cliBinPath); err == nil {
		_ = os.Chmod(cliBinPath, 0755)
	}

	// 6. Symlink server (and cli if present) into the shared bin dir.
	binDir := paths.BinDir(v.serverRoot)
	if err := LinkIntoBinDir(binDir, "valkey-server", binPath); err != nil {
		fmt.Fprintf(w, "valkey: warning: %v\n", err)
	}
	if fileExists(cliBinPath) {
		if err := LinkIntoBinDir(binDir, "valkey-cli", cliBinPath); err != nil {
			fmt.Fprintf(w, "valkey: warning: %v\n", err)
		}
	}

	// 7. Write config.env with static connection info.
	envPath := filepath.Join(valkeyDir, "config.env")
	envContent := "REDIS_HOST=127.0.0.1\nREDIS_PORT=6379\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("valkey: write config.env: %w", err)
	}

	fmt.Fprintln(w, "valkey: install complete")
	return nil
}

func (v *ValkeyInstaller) Purge(ctx context.Context) error {
	return v.PurgeW(ctx, io.Discard)
}

func (v *ValkeyInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := v.supervisor.Stop("redis"); err != nil {
		fmt.Fprintf(w, "valkey: warning: stop process: %v\n", err)
	}

	// Remove bin dir symlinks.
	binDir := paths.BinDir(v.serverRoot)
	UnlinkFromBinDir(binDir, "valkey-server")
	UnlinkFromBinDir(binDir, "valkey-cli")

	// Remove the directory.
	valkeyDir := paths.ServiceDir(v.serverRoot, "valkey")
	if err := os.RemoveAll(valkeyDir); err != nil {
		return fmt.Errorf("valkey: remove dir: %w", err)
	}

	fmt.Fprintln(w, "valkey: purge complete")
	return nil
}

// extractFromTarXz extracts all files from a .tar.xz archive into destDir,
// stripping the first path component (the versioned top-level directory).
// For example: mysql-8.4.7-.../bin/mysqld → destDir/bin/mysqld.
func extractFromTarXz(tarXzPath, destDir string) error {
	f, err := os.Open(tarXzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("xz reader: %w", err)
	}

	tr := tar.NewReader(xzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Strip the leading path component (e.g. "mysql-8.4.7-.../").
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue // skip the top-level directory entry itself
		}
		relPath := parts[1]
		destPath := filepath.Join(destDir, relPath)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, os.FileMode(hdr.Mode)|0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", destPath, err)
			}
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create %s: %w", destPath, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("write %s: %w", destPath, err)
			}
			out.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("mkdir parent for symlink %s: %w", destPath, err)
			}
			_ = os.Remove(destPath) // remove stale symlink if it exists
			if err := os.Symlink(hdr.Linkname, destPath); err != nil {
				return fmt.Errorf("symlink %s: %w", destPath, err)
			}
		}
	}
	return nil
}

// extractFromTar finds the first entry whose base name matches binaryName in a
// .tar.gz archive and writes it to destPath.
func extractFromTar(tarPath, binaryName, destPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && strings.TrimSuffix(filepath.Base(hdr.Name), ".exe") == binaryName {
			out, err := os.Create(destPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("%s not found in archive", binaryName)
}
