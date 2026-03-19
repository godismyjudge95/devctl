package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

// mysqlVersion is the MySQL 8.4 LTS version to install.
// Update this constant to pick up a newer release.
const mysqlVersion = "8.4.8"

// mysqlDebURL returns the CDN URL for one of the Ubuntu-specific MySQL
// community .deb packages. These debs bundle their own private copies of
// libabsl / libprotobuf-lite under usr/lib/mysql/private/, so no MySQL
// libraries need to be installed system-wide.
func mysqlDebURL(pkg string) string {
	return fmt.Sprintf(
		"https://repo.mysql.com/apt/ubuntu/pool/mysql-8.4-lts/m/mysql-community/%s_%s-1ubuntu24.04_amd64.deb",
		pkg, mysqlVersion,
	)
}

// MySQLInstaller downloads the Ubuntu-specific MySQL 8.4 LTS .deb packages
// and extracts their contents directly into {serverRoot}/mysql/, keeping
// MySQL fully self-contained in the devctl server directory.
//
// Only libnuma1 is installed system-wide via APT — it is a tiny NUMA policy
// library (≈23 kB) that the MySQL binary requires and that is not bundled
// inside the deb packages.
type MySQLInstaller struct {
	supervisor *services.Supervisor
	serverRoot string
	siteUser   string
}

func (m *MySQLInstaller) ServiceID() string { return "mysql" }

func (m *MySQLInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(m.serverRoot, "mysql"), "bin", "mysqld"))
}

func (m *MySQLInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MySQLInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "mysql: already installed")
		return nil
	}

	mysqlDir := paths.ServiceDir(m.serverRoot, "mysql")
	binDir := filepath.Join(mysqlDir, "bin")
	libDir := filepath.Join(mysqlDir, "lib")
	dataDir := filepath.Join(mysqlDir, "data")

	// 1. Install libnuma1 — the one system library not bundled in the debs.
	fmt.Fprintln(w, "mysql: installing libnuma1...")
	if err := aptInstallW(ctx, w, "libnuma1"); err != nil {
		return fmt.Errorf("mysql: install libnuma1: %w", err)
	}

	// 2. Create directories.
	fmt.Fprintln(w, "mysql: creating directories...")
	for _, dir := range []string{binDir, libDir, dataDir, filepath.Join(mysqlDir, "mysql-files")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("mysql: create dir %s: %w", dir, err)
		}
	}

	// 3. Download and extract the three deb packages.
	//    - server-core: mysqld + private libabsl/libprotobuf .so files
	//    - client-core: mysql, mysqldump
	//    - client:      mysqladmin
	debs := []struct {
		pkg  string
		desc string
	}{
		{"mysql-community-server-core", "server"},
		{"mysql-community-client-core", "client-core"},
		{"mysql-community-client", "client"},
	}

	for _, d := range debs {
		tmpDeb := filepath.Join(os.TempDir(), fmt.Sprintf("mysql-%s-%s.deb", d.pkg, mysqlVersion))
		defer os.Remove(tmpDeb)

		fmt.Fprintf(w, "mysql: downloading %s...\n", d.desc)
		if err := curlDownloadW(ctx, w, mysqlDebURL(d.pkg), tmpDeb); err != nil {
			return fmt.Errorf("mysql: download %s: %w", d.pkg, err)
		}

		fmt.Fprintf(w, "mysql: extracting %s...\n", d.desc)
		if err := extractMySQLDeb(tmpDeb, binDir, libDir); err != nil {
			return fmt.Errorf("mysql: extract %s: %w", d.pkg, err)
		}
	}

	// 4. Write mysql.env so the supervisor sets LD_LIBRARY_PATH, allowing
	//    mysqld to find its bundled private .so files at runtime.
	fmt.Fprintln(w, "mysql: writing mysql.env...")
	envContent := fmt.Sprintf("LD_LIBRARY_PATH=%s\n", libDir)
	if err := os.WriteFile(filepath.Join(mysqlDir, "mysql.env"), []byte(envContent), 0644); err != nil {
		return fmt.Errorf("mysql: write mysql.env: %w", err)
	}

	// 5. Write my.cnf — both [client] and [mysqld] sections so that CLI tools
	//    (mysql, mysqldump, mysqladmin) automatically find the socket without
	//    needing an explicit --socket flag.
	fmt.Fprintln(w, "mysql: writing my.cnf...")
	sockPath := filepath.Join(mysqlDir, "mysql.sock")
	myCnf := fmt.Sprintf(
		"[client]\nsocket=%s\n\n[mysqld]\nbasedir=%s\ndatadir=%s\nsocket=%s\npid-file=%s\nlog-error=%s\nport=3306\nbind-address=127.0.0.1\nsecure-file-priv=%s\n",
		sockPath,
		mysqlDir,
		dataDir,
		sockPath,
		filepath.Join(mysqlDir, "mysql.pid"),
		filepath.Join(mysqlDir, "mysql-error.log"),
		filepath.Join(mysqlDir, "mysql-files"),
	)
	if err := os.WriteFile(filepath.Join(mysqlDir, "my.cnf"), []byte(myCnf), 0644); err != nil {
		return fmt.Errorf("mysql: write my.cnf: %w", err)
	}

	// 6. Initialise the data directory.
	fmt.Fprintln(w, "mysql: initialising data directory...")
	initCmd := fmt.Sprintf(
		"LD_LIBRARY_PATH=%s %s --initialize-insecure --user=root --datadir=%s --basedir=%s",
		libDir,
		filepath.Join(binDir, "mysqld"),
		dataDir,
		mysqlDir,
	)
	if out, err := runShellW(ctx, w, initCmd); err != nil {
		return fmt.Errorf("mysql: initialize: %w\n%s", err, out)
	}

	// 7. Write config.env for the credentials panel.
	fmt.Fprintln(w, "mysql: writing config.env...")
	credContent := "DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_USERNAME=root\nDB_PASSWORD=\n"
	if err := os.WriteFile(filepath.Join(mysqlDir, "config.env"), []byte(credContent), 0600); err != nil {
		return fmt.Errorf("mysql: write config.env: %w", err)
	}

	// 8. Symlink client binaries into the shared bin dir so they are in PATH.
	fmt.Fprintln(w, "mysql: symlinking client binaries...")
	sharedBinDir := paths.BinDir(m.serverRoot)
	for _, bin := range []string{"mysql", "mysqldump", "mysqladmin"} {
		if err := LinkIntoBinDir(sharedBinDir, bin, filepath.Join(binDir, bin)); err != nil {
			fmt.Fprintf(w, "mysql: warning: symlink %s: %v\n", bin, err)
		}
	}

	// 9. Transfer ownership to the site user so the MySQL process (running
	//    as root but inside the devctl server dir) and the user's CLI tools
	//    can both access the files without permission errors.
	if m.siteUser != "" {
		fmt.Fprintf(w, "mysql: chowning %s to %s...\n", mysqlDir, m.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", m.siteUser, m.siteUser, mysqlDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("mysql: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "mysql: install complete")
	return nil
}

func (m *MySQLInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard, false)
}

func (m *MySQLInstaller) PurgeW(ctx context.Context, w io.Writer, preserveData bool) error {
	if err := m.supervisor.Stop("mysql"); err != nil {
		fmt.Fprintf(w, "mysql: warning: stop process: %v\n", err)
	}

	// Remove bin dir symlinks.
	sharedBinDir := paths.BinDir(m.serverRoot)
	for _, bin := range []string{"mysql", "mysqldump", "mysqladmin"} {
		UnlinkFromBinDir(sharedBinDir, bin)
	}

	mysqlDir := paths.ServiceDir(m.serverRoot, "mysql")
	if preserveData {
		// Remove everything in mysqlDir except the data/ subdirectory.
		fmt.Fprintln(w, "mysql: purging binaries (preserving data/)...")
		if err := removeAllExcept(mysqlDir, "data"); err != nil {
			return fmt.Errorf("mysql: remove binaries: %w", err)
		}
	} else {
		if err := os.RemoveAll(mysqlDir); err != nil {
			return fmt.Errorf("mysql: remove dir: %w", err)
		}
	}

	fmt.Fprintln(w, "mysql: purge complete")
	return nil
}

// extractMySQLDeb reads a .deb archive and extracts:
//   - usr/sbin/mysqld and usr/bin/mysql* → binDir
//   - usr/lib/mysql/private/*.so          → libDir
//
// A .deb is an ar(1) archive containing data.tar.xz (or .gz/.zst).
// We shell out to dpkg-deb to avoid implementing ar parsing in Go.
func extractMySQLDeb(debPath, binDir, libDir string) error {
	tmpDir, err := os.MkdirTemp("", "mysql-deb-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// dpkg-deb --extract unpacks the full file tree under tmpDir.
	ctx := context.Background()
	if out, err := runShell(ctx, fmt.Sprintf("dpkg-deb --extract %s %s", debPath, tmpDir)); err != nil {
		return fmt.Errorf("dpkg-deb extract: %w\n%s", err, out)
	}

	// Copy usr/sbin/mysqld → binDir/mysqld
	// Copy usr/bin/mysql*  → binDir/
	for _, srcDir := range []string{
		filepath.Join(tmpDir, "usr", "sbin"),
		filepath.Join(tmpDir, "usr", "bin"),
	} {
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "mysql") {
				continue
			}
			if err := copyFile(filepath.Join(srcDir, e.Name()), filepath.Join(binDir, e.Name())); err != nil {
				return err
			}
		}
	}

	// Copy usr/lib/mysql/private/*.so → libDir/
	privateDir := filepath.Join(tmpDir, "usr", "lib", "mysql", "private")
	entries, err := os.ReadDir(privateDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(privateDir, e.Name()), filepath.Join(libDir, e.Name())); err != nil {
			return err
		}
	}

	return nil
}

// copyFile copies src to dst, preserving the source file mode.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return nil
}
