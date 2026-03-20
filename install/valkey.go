package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/ulikunitz/xz"
)

//go:embed valkey.conf
var valkeyConfTemplate []byte

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
	siteUser   string
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

	// 8. Write valkey.conf with full defaults.
	fmt.Fprintln(w, "valkey: writing valkey.conf...")
	if err := writeValkeyConf(valkeyDir); err != nil {
		return fmt.Errorf("valkey: write valkey.conf: %w", err)
	}

	// 10. Transfer ownership to the site user.
	if v.siteUser != "" {
		fmt.Fprintf(w, "valkey: chowning %s to %s...\n", valkeyDir, v.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", v.siteUser, v.siteUser, valkeyDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("valkey: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "valkey: install complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest Valkey version.
func (v *ValkeyInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchGitHubLatestVersion(ctx, "valkey-io/valkey")
}

// UpdateW stops Valkey, replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (v *ValkeyInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := v.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("valkey: update: %w", err)
	}
	// Valkey tags have no "v" prefix (e.g. "9.0.3"). Detect which distro to use.
	codename, _ := lsbReleaseName(ctx)
	distro := "jammy"
	if codename == "noble" {
		distro = "noble"
	}
	dlURL := fmt.Sprintf("https://download.valkey.io/releases/valkey-%s-%s-x86_64.tar.gz", latest, distro)

	valkeyDir := paths.ServiceDir(v.serverRoot, "valkey")
	binPath := filepath.Join(valkeyDir, "valkey-server")
	cliBinPath := filepath.Join(valkeyDir, "valkey-cli")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("valkey-%s-update.tar.gz", latest))
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "valkey: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("valkey: update download: %w", err)
	}

	fmt.Fprintln(w, "valkey: stopping valkey...")
	if err := v.supervisor.Stop("redis"); err != nil {
		fmt.Fprintf(w, "valkey: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "valkey: replacing binary...")
	if err := extractFromTar(tmpTar, "valkey-server", binPath); err != nil {
		return fmt.Errorf("valkey: update extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("valkey: update chmod: %w", err)
	}
	// Also update valkey-cli if present.
	if err := extractFromTar(tmpTar, "valkey-cli", cliBinPath); err == nil {
		_ = os.Chmod(cliBinPath, 0755)
	}

	fmt.Fprintf(w, "valkey: binary replaced with %s\n", latest)
	return nil
}

func (v *ValkeyInstaller) Purge(ctx context.Context) error {
	return v.PurgeW(ctx, io.Discard, false)
}

func (v *ValkeyInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
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

// EnsureValkeyConf writes valkey.conf to the Valkey service directory if the
// file is missing. Safe to call on every startup — it is a no-op when the file
// already exists. Use this to migrate installs that pre-date config-file support.
func EnsureValkeyConf(serverRoot string) error {
	return writeValkeyConf(paths.ServiceDir(serverRoot, "valkey"))
}

// writeValkeyConf writes valkey.conf to dir/valkey.conf using the official
// Valkey 9.0.3 default config as a base, then stamps in devctl-specific values.
// The file is only written if it does not yet exist so user edits are preserved.
func writeValkeyConf(dir string) error {
	confPath := filepath.Join(dir, "valkey.conf")
	if _, err := os.Stat(confPath); err == nil {
		return nil // already exists — don't overwrite
	}

	// Start from the official Valkey default config and apply our overrides.
	// The official file already has `bind 127.0.0.1 -::1` and `port 6379`;
	// we override bind to IPv4-only and ensure daemonize is off.
	overrides := map[string]string{
		"bind":      "127.0.0.1",
		"port":      "6379",
		"daemonize": "no",
		"logfile":   `""`,
		"dir":       "./",
	}

	lines := strings.Split(string(valkeyConfTemplate), "\n")
	applied := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comments and blank lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 1 {
			continue
		}
		key := parts[0]
		if newVal, ok := overrides[key]; ok && !applied[key] {
			lines[i] = key + " " + newVal
			applied[key] = true
		}
	}

	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
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
