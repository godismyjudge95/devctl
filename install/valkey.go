package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danielgormly/devctl/dist"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/ulikunitz/xz"
)

//go:embed valkey.conf
var valkeyConfTemplate []byte

// valkeyDownloadURL is the vendor archive for this platform.
// Linux: official valkey.io jammy/noble tarballs.
// Darwin: Laravel Herd redistributes a universal Mach-O zip from
// download.herdphp.com — valkey.io has no macOS artifact.
func valkeyDownloadURL(ctx context.Context, version string) (string, error) {
	a, err := dist.For("valkey")
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return "https://download.herdphp.com/services/valkey/" + a.File, nil
	}
	distro := a.Token
	if strings.HasPrefix(distro, "jammy-") {
		codename, _ := lsbReleaseName(ctx)
		if codename == "noble" {
			distro = "noble-" + strings.TrimPrefix(distro, "jammy-")
		}
	}
	return fmt.Sprintf("https://download.valkey.io/releases/valkey-%s-%s.tar.gz", version, distro), nil
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
	if _, err := dist.For("valkey"); err != nil {
		return fmt.Errorf("valkey: %w", err)
	}

	latest, err := v.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("valkey: resolve latest version: %w", err)
	}

	valkeyDir := paths.ServiceDir(v.serverRoot, "valkey")
	binPath := filepath.Join(valkeyDir, "valkey-server")
	dlURL, err := valkeyDownloadURL(ctx, latest)
	if err != nil {
		return err
	}
	tmpArchive := filepath.Join(os.TempDir(), filepath.Base(dlURL))
	defer os.Remove(tmpArchive)

	// 1. Create directory.
	fmt.Fprintln(w, "valkey: creating directory...")
	if err := os.MkdirAll(valkeyDir, 0755); err != nil {
		return fmt.Errorf("valkey: create dir: %w", err)
	}

	fmt.Fprintf(w, "valkey: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpArchive); err != nil {
		return fmt.Errorf("valkey: download: %w", err)
	}

	fmt.Fprintln(w, "valkey: extracting binary...")
	if strings.HasSuffix(dlURL, ".zip") {
		if err := extractValkeyZip(tmpArchive, valkeyDir); err != nil {
			return fmt.Errorf("valkey: extract zip: %w", err)
		}
	} else {
		if err := extractFromTar(tmpArchive, "valkey-server", binPath); err != nil {
			return fmt.Errorf("valkey: extract: %w", err)
		}
		cliBinPath := filepath.Join(valkeyDir, "valkey-cli")
		if err := extractFromTar(tmpArchive, "valkey-cli", cliBinPath); err == nil {
			_ = os.Chmod(cliBinPath, 0755)
		}
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("valkey: chmod binary: %w", err)
	}

	cliBinPath := filepath.Join(valkeyDir, "valkey-cli")

	// Symlink server (and cli if present) into the shared bin dir.
	// Both valkey-{server,cli} and redis-{server,cli} aliases are created so
	// that muscle-memory commands and scripts that reference the Redis names
	// continue to work out of the box.
	binDir := paths.BinDir(v.serverRoot)
	for _, link := range []string{"valkey-server", "redis-server"} {
		if err := LinkIntoBinDir(binDir, link, binPath); err != nil {
			fmt.Fprintf(w, "valkey: warning: %v\n", err)
		}
	}
	if fileExists(cliBinPath) {
		for _, link := range []string{"valkey-cli", "redis-cli"} {
			if err := LinkIntoBinDir(binDir, link, cliBinPath); err != nil {
				fmt.Fprintf(w, "valkey: warning: %v\n", err)
			}
		}
	}

	// 7. Write config.env with static connection info.
	envPath := filepath.Join(valkeyDir, "config.env")
	envContent := "REDIS_HOST=127.0.0.1\nREDIS_PORT=6379\nREDIS_PASSWORD=\n"
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
		chownCmd := chownRecursiveCmd(v.siteUser, valkeyDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("valkey: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "valkey: install complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest Valkey version.
// If the context carries a pre-resolved version (via install.WithPreResolvedVersion),
// that value is returned immediately without hitting GitHub.
func (v *ValkeyInstaller) LatestVersion(ctx context.Context) (string, error) {
	if runtime.GOOS == "darwin" {
		a, err := dist.For("valkey")
		if err != nil {
			return "", err
		}
		return a.Token, nil
	}
	if v2 := preResolvedVersionFromCtx(ctx); v2 != "" {
		return v2, nil
	}
	return fetchGitHubLatestVersion(ctx, "valkey-io/valkey")
}

// UpdateW stops Valkey, replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (v *ValkeyInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := v.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("valkey: update: %w", err)
	}
	dlURL, err := valkeyDownloadURL(ctx, latest)
	if err != nil {
		return err
	}

	valkeyDir := paths.ServiceDir(v.serverRoot, "valkey")
	binPath := filepath.Join(valkeyDir, "valkey-server")
	cliBinPath := filepath.Join(valkeyDir, "valkey-cli")
	tmpArchive := filepath.Join(os.TempDir(), filepath.Base(dlURL))
	defer os.Remove(tmpArchive)

	fmt.Fprintf(w, "valkey: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpArchive); err != nil {
		return fmt.Errorf("valkey: update download: %w", err)
	}

	fmt.Fprintln(w, "valkey: stopping valkey...")
	if err := v.supervisor.Stop("redis"); err != nil {
		fmt.Fprintf(w, "valkey: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "valkey: replacing binary...")
	if strings.HasSuffix(dlURL, ".zip") {
		if err := extractValkeyZip(tmpArchive, valkeyDir); err != nil {
			return fmt.Errorf("valkey: update extract: %w", err)
		}
	} else {
		if err := extractFromTar(tmpArchive, "valkey-server", binPath); err != nil {
			return fmt.Errorf("valkey: update extract: %w", err)
		}
		_ = extractFromTar(tmpArchive, "valkey-cli", cliBinPath)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("valkey: update chmod: %w", err)
	}
	if fileExists(cliBinPath) {
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

	// Remove bin dir symlinks (both valkey-* and redis-* aliases).
	binDir := paths.BinDir(v.serverRoot)
	for _, link := range []string{"valkey-server", "redis-server", "valkey-cli", "redis-cli"} {
		UnlinkFromBinDir(binDir, link)
	}

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

// extractFromTarGzStrip extracts a .tar.gz into destDir, stripping the first
// path component (the versioned top-level directory).
func extractFromTarGzStrip(tarGzPath, destDir string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
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
			_ = os.Remove(destPath)
			if err := os.Symlink(hdr.Linkname, destPath); err != nil {
				return fmt.Errorf("symlink %s: %w", destPath, err)
			}
		}
	}
	return nil
}

// .tar.gz archive and writes it to destPath.
//
// When multiple matches exist (e.g. ClickHouse ships a tiny bash-completion
// script named "clickhouse" before usr/bin/clickhouse), the largest regular
// file wins. Ties prefer a path containing "/bin/".
func extractFromTar(tarPath, binaryName, destPath string) error {
	bestName, err := findBestTarMember(tarPath, binaryName)
	if err != nil {
		return err
	}
	return extractNamedFromTar(tarPath, bestName, destPath)
}

// findBestTarMember returns the archive member path to extract for binaryName.
func findBestTarMember(tarPath, binaryName string) (string, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	var bestName string
	var bestSize int64 = -1
	var bestInBin bool

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.TrimSuffix(filepath.Base(hdr.Name), ".exe") != binaryName {
			continue
		}
		inBin := strings.Contains(hdr.Name, "/bin/")
		// Prefer larger files; on size ties prefer a /bin/ path (real binary over completion scripts).
		if hdr.Size > bestSize || (hdr.Size == bestSize && inBin && !bestInBin) {
			bestName = hdr.Name
			bestSize = hdr.Size
			bestInBin = inBin
		}
	}
	if bestName == "" {
		return "", fmt.Errorf("%s not found in archive", binaryName)
	}
	return bestName, nil
}

// extractNamedFromTar extracts a single named member from a .tar.gz to destPath.
func extractNamedFromTar(tarPath, memberName, destPath string) error {
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
		if hdr.Name != memberName || hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Remove the old file before creating the new one.
		// On Linux, overwriting a running executable in-place (os.Create/truncate)
		// returns ETXTBSY. Unlinking first lets the kernel keep the old inode open
		// while we write the replacement at a fresh inode.
		_ = os.Remove(destPath)
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
	return fmt.Errorf("%s not found in archive", memberName)
}

// extractValkeyZip copies files from the Herd universal zip (bin/valkey-server
// plus bundled libssl/libcrypto) into destDir so @executable_path loads.
func extractValkeyZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if base == "" || base == "." || base == ".." {
			continue
		}
		dest := filepath.Join(destDir, base)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); copyErr == nil {
			copyErr = cerr
		}
		if copyErr != nil {
			return copyErr
		}
	}
	if !fileExists(filepath.Join(destDir, "valkey-server")) {
		return fmt.Errorf("zip has no valkey-server")
	}
	return nil
}
