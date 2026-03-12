package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MySQLInstaller installs MySQL 8.4 LTS from the official MySQL APT
// repository.  We download the mysql-apt-config deb, pre-seed debconf to
// select mysql-8.4-lts, install it, then install the server.
//
// Ref: https://dev.mysql.com/doc/mysql-installation-excerpt/8.4/en/linux-installation-apt-repo.html
type MySQLInstaller struct{}

func (m *MySQLInstaller) ServiceID() string { return "mysql" }

func (m *MySQLInstaller) IsInstalled() bool {
	return fileExists("/usr/sbin/mysqld") || fileExists("/usr/bin/mysqld_safe")
}

func (m *MySQLInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MySQLInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		return nil
	}

	if err := aptInstallW(ctx, w, "wget", "lsb-release"); err != nil {
		return err
	}

	// Download the apt-config deb to a temp file.
	tmp := filepath.Join(os.TempDir(), "mysql-apt-config.deb")
	defer os.Remove(tmp)

	const aptConfigURL = "https://dev.mysql.com/get/mysql-apt-config_0.8.36-1_all.deb"
	if err := curlDownloadW(ctx, w, aptConfigURL, tmp); err != nil {
		return err
	}

	// Pre-seed debconf to avoid the interactive prompt.
	if err := debconfSeedW(ctx, w,
		"mysql-apt-config mysql-apt-config/select-server select mysql-8.4-lts",
		"mysql-apt-config mysql-apt-config/select-product select Ok",
	); err != nil {
		return err
	}

	// Install the apt-config deb.
	if err := dpkgInstallW(ctx, w, tmp); err != nil {
		return err
	}

	if err := aptUpdateW(ctx, w); err != nil {
		return err
	}

	if err := aptInstallW(ctx, w, "mysql-server", "mysql-client"); err != nil {
		return err
	}

	return enableAndStartW(ctx, w, "mysql")
}

func (m *MySQLInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard)
}

func (m *MySQLInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	stopAndDisableW(ctx, w, "mysql")
	if err := aptPurgeW(ctx, w, "mysql-server", "mysql-client", "mysql-common", "mysql-apt-config"); err != nil {
		return err
	}
	removeFiles(
		"/etc/apt/sources.list.d/mysql.list",
		"/usr/share/keyrings/mysql-archive-keyring.gpg",
	)
	_ = os.RemoveAll("/var/lib/mysql")
	_ = os.RemoveAll("/etc/mysql")
	return nil
}

// debconfSeed pre-seeds debconf answers. Each entry is a full debconf line
// like "pkg key type value".
func debconfSeed(ctx context.Context, entries ...string) error {
	return debconfSeedW(ctx, io.Discard, entries...)
}

// debconfSeedW is like debconfSeed but streams output to w.
func debconfSeedW(ctx context.Context, w io.Writer, entries ...string) error {
	for _, entry := range entries {
		out, err := runShellW(ctx, w, fmt.Sprintf(`echo "%s" | debconf-set-selections`, entry))
		if err != nil {
			return wrapOutput("debconf-set-selections", err, out)
		}
	}
	return nil
}

// dpkgInstall runs dpkg -i <path>.
func dpkgInstall(ctx context.Context, path string) error {
	return dpkgInstallW(ctx, io.Discard, path)
}

// dpkgInstallW is like dpkgInstall but streams output to w.
func dpkgInstallW(ctx context.Context, w io.Writer, path string) error {
	out, err := runShellW(ctx, w, fmt.Sprintf("dpkg -i %s", path))
	if err != nil {
		return wrapOutput("dpkg -i "+path, err, out)
	}
	return nil
}
