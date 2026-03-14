package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/services"
)

// mysqlVersion is the MySQL 8.4 LTS version to download.
// Update this constant to pick up a newer release.
const mysqlVersion = "8.4.7"

// mysqlTarURL returns the download URL for the MySQL minimal generic Linux
// tarball. The "minimal" variant strips debug symbols and reduces the download
// to ~65 MB instead of ~875 MB for the full build.
func mysqlTarURL() string {
	return fmt.Sprintf(
		"https://cdn.mysql.com/archives/mysql-8.4/mysql-%s-linux-glibc2.28-x86_64-minimal.tar.xz",
		mysqlVersion,
	)
}

// MySQLInstaller downloads the MySQL 8.4 LTS "Linux Generic" minimal binary
// tarball to $HOME/sites/server/mysql/, initialises the data directory, and
// runs mysqld as a supervised child process of devctl.
//
// No PPA, no APT packages, no systemd unit for MySQL itself.
// libaio (required by the generic binary) is installed via APT as a lightweight
// system dependency.
type MySQLInstaller struct {
	supervisor *services.Supervisor
	siteHome   string // home directory of the non-root site user (e.g. "/home/alice")
}

func (m *MySQLInstaller) ServiceID() string { return "mysql" }

func (m *MySQLInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(m.siteHome, "sites", "server", "mysql", "bin", "mysqld"))
}

func (m *MySQLInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MySQLInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "mysql: already installed")
		return nil
	}

	mysqlDir := filepath.Join(m.siteHome, "sites", "server", "mysql")
	dataDir := filepath.Join(mysqlDir, "data")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("mysql-%s-minimal.tar.xz", mysqlVersion))
	defer os.Remove(tmpTar)

	// 1. Install libaio — required by the MySQL generic binary.
	//    Ubuntu 24.04 (Noble) ships libaio1t64; Ubuntu 22.04 and Debian ship libaio1.
	fmt.Fprintln(w, "mysql: installing libaio system dependency...")
	if err := aptInstallW(ctx, w, "libaio1t64"); err != nil {
		fmt.Fprintln(w, "mysql: libaio1t64 not found, trying libaio1...")
		if err2 := aptInstallW(ctx, w, "libaio1"); err2 != nil {
			return fmt.Errorf("mysql: install libaio: %w", err2)
		}
	}

	// 2. Create directories.
	fmt.Fprintln(w, "mysql: creating directories...")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return fmt.Errorf("mysql: create data dir: %w", err)
	}

	// 3. Download the minimal tarball.
	fmt.Fprintf(w, "mysql: downloading %s...\n", mysqlVersion)
	if err := curlDownloadW(ctx, w, mysqlTarURL(), tmpTar); err != nil {
		return fmt.Errorf("mysql: download: %w", err)
	}

	// 4. Extract the full tarball into mysqlDir, stripping the versioned top-level dir.
	fmt.Fprintln(w, "mysql: extracting tarball...")
	if err := extractFromTarXz(tmpTar, mysqlDir); err != nil {
		return fmt.Errorf("mysql: extract: %w", err)
	}

	// 5. Write my.cnf so mysqld knows where everything lives.
	fmt.Fprintln(w, "mysql: writing my.cnf...")
	myCnf := fmt.Sprintf(
		"[mysqld]\nbasedir=%s\ndatadir=%s\nsocket=%s\npid-file=%s\nlog-error=%s\nport=3306\nbind-address=127.0.0.1\n",
		mysqlDir,
		dataDir,
		filepath.Join(mysqlDir, "mysql.sock"),
		filepath.Join(mysqlDir, "mysql.pid"),
		filepath.Join(mysqlDir, "mysql-error.log"),
	)
	if err := os.WriteFile(filepath.Join(mysqlDir, "my.cnf"), []byte(myCnf), 0644); err != nil {
		return fmt.Errorf("mysql: write my.cnf: %w", err)
	}

	// 6. Initialise the data directory (creates system tables, no root password).
	fmt.Fprintln(w, "mysql: initialising data directory...")
	initCmd := fmt.Sprintf(
		"%s --initialize-insecure --user=root --datadir=%s --basedir=%s",
		filepath.Join(mysqlDir, "bin", "mysqld"),
		dataDir,
		mysqlDir,
	)
	out, err := runShellW(ctx, w, initCmd)
	if err != nil {
		return fmt.Errorf("mysql: initialize: %w\n%s", err, out)
	}

	// 7. Write config.env with connection info for the credentials panel.
	fmt.Fprintln(w, "mysql: writing config.env...")
	envContent := "DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_USERNAME=root\nDB_PASSWORD=\n"
	if err := os.WriteFile(filepath.Join(mysqlDir, "config.env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("mysql: write config.env: %w", err)
	}

	fmt.Fprintln(w, "mysql: install complete")
	return nil
}

func (m *MySQLInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard)
}

func (m *MySQLInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := m.supervisor.Stop("mysql"); err != nil {
		fmt.Fprintf(w, "mysql: warning: stop process: %v\n", err)
	}

	// Remove the entire mysql directory (binary tree + data + config).
	mysqlDir := filepath.Join(m.siteHome, "sites", "server", "mysql")
	if err := os.RemoveAll(mysqlDir); err != nil {
		return fmt.Errorf("mysql: remove dir: %w", err)
	}

	fmt.Fprintln(w, "mysql: purge complete")
	return nil
}
