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

	"github.com/danielgormly/devctl/services"
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
// $HOME/sites/server/valkey/ and runs it as a supervised child process.
// Valkey is a Redis-compatible open-source fork; the service ID is kept as
// "redis" so existing Laravel .env files (REDIS_HOST, etc.) continue to work.
type ValkeyInstaller struct {
	supervisor *services.Supervisor
	siteHome   string // home directory of the non-root site user (e.g. "/home/alice")
}

func (v *ValkeyInstaller) ServiceID() string { return "redis" }

func (v *ValkeyInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(v.siteHome, "sites", "server", "valkey", "valkey-server"))
}

func (v *ValkeyInstaller) Install(ctx context.Context) error {
	return v.InstallW(ctx, io.Discard)
}

func (v *ValkeyInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if v.IsInstalled() {
		fmt.Fprintln(w, "valkey: already installed")
		return nil
	}

	valkeyDir := filepath.Join(v.siteHome, "sites", "server", "valkey")
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

	// 5. Write config.env with static connection info.
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

	// Remove the directory.
	valkeyDir := filepath.Join(v.siteHome, "sites", "server", "valkey")
	if err := os.RemoveAll(valkeyDir); err != nil {
		return fmt.Errorf("valkey: remove dir: %w", err)
	}

	fmt.Fprintln(w, "valkey: purge complete")
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
