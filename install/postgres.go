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

	"github.com/danielgormly/devctl/services"
)

// postgresVersion is the Percona Distribution for PostgreSQL full version to
// download. Update this constant to pick up a newer release; the major version
// (postgresMajor) must match the leading number.
//
// Download page: https://downloads.percona.com/downloads/postgresql-distribution-18/
// Tarball docs:  https://docs.percona.com/postgresql/18/tarball.html
const (
	postgresVersion = "18.3"
	postgresMajor   = "18"
)

// perconaTarURL constructs the download URL for the Percona PostgreSQL binary
// tarball. The ssl3 variant targets OpenSSL 3.x (Ubuntu 22.04+, Debian
// bookworm+). Arch: amd64 → x86_64, arm64 → aarch64.
func perconaTarURL() string {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	return fmt.Sprintf(
		"https://downloads.percona.com/downloads/postgresql-distribution-%s/%s/binary/tarball/percona-postgresql-%s-ssl3-linux-%s.tar.gz",
		postgresMajor,
		postgresVersion,
		postgresVersion,
		arch,
	)
}

// PostgresInstaller downloads the Percona Distribution for PostgreSQL binary
// tarball to $HOME/sites/server/postgres/, initialises the data directory as
// siteUser (PostgreSQL refuses to start as root), and runs postgres as a
// supervised child process of devctl with privilege drop via ManagedUser.
//
// No APT packages for PostgreSQL itself — only libreadline-dev is installed as
// a system library required by the tarball. No systemd unit.
type PostgresInstaller struct {
	supervisor *services.Supervisor
	siteHome   string // home directory of the non-root site user (e.g. "/home/alice")
	siteUser   string // username of the non-root site user (e.g. "alice")
}

func (p *PostgresInstaller) ServiceID() string { return "postgres" }

func (p *PostgresInstaller) postgresDir() string {
	return filepath.Join(p.siteHome, "sites", "server", "postgres")
}

// IsInstalled returns true when the postgres server binary is present.
func (p *PostgresInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(p.postgresDir(), "bin", "postgres"))
}

func (p *PostgresInstaller) Install(ctx context.Context) error {
	return p.InstallW(ctx, io.Discard)
}

func (p *PostgresInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if p.IsInstalled() {
		fmt.Fprintln(w, "postgres: already installed")
		return nil
	}

	pgDir := p.postgresDir()
	dataDir := filepath.Join(pgDir, "data")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("percona-postgresql-%s.tar.gz", postgresVersion))
	defer os.Remove(tmpTar)

	// 1. Install libreadline-dev — required by the Percona tarball binaries.
	fmt.Fprintln(w, "postgres: installing libreadline-dev...")
	if err := aptInstallW(ctx, w, "libreadline-dev"); err != nil {
		return fmt.Errorf("postgres: install libreadline-dev: %w", err)
	}

	// 2. Create the install directory.
	fmt.Fprintln(w, "postgres: creating directories...")
	if err := os.MkdirAll(pgDir, 0755); err != nil {
		return fmt.Errorf("postgres: create dir: %w", err)
	}

	// 3. Download the Percona tarball (~500 MB).
	url := perconaTarURL()
	fmt.Fprintf(w, "postgres: downloading Percona PostgreSQL %s...\n", postgresVersion)
	if err := curlDownloadW(ctx, w, url, tmpTar); err != nil {
		return fmt.Errorf("postgres: download: %w", err)
	}

	// 4. Extract only the percona-postgresql<major>/ subtree from the tarball,
	//    stripping that prefix so bin/, lib/, share/, etc. land directly in pgDir.
	fmt.Fprintln(w, "postgres: extracting tarball (server only)...")
	subtree := fmt.Sprintf("percona-postgresql%s", postgresMajor)
	if err := extractPercona(tmpTar, subtree, pgDir); err != nil {
		return fmt.Errorf("postgres: extract: %w", err)
	}

	// 5. Transfer ownership of the entire postgres directory to siteUser.
	//    PostgreSQL refuses to start (and initdb refuses to run) as root.
	fmt.Fprintf(w, "postgres: transferring ownership to %s...\n", p.siteUser)
	chownCmd := fmt.Sprintf("chown -R %s:%s %s", p.siteUser, p.siteUser, pgDir)
	if out, err := runShellW(ctx, w, chownCmd); err != nil {
		return fmt.Errorf("postgres: chown: %w\n%s", err, out)
	}

	// 6. Initialise the data directory as siteUser (idempotent: skip if already done).
	if !fileExists(filepath.Join(dataDir, "PG_VERSION")) {
		fmt.Fprintln(w, "postgres: initialising data directory...")
		initCmd := fmt.Sprintf(
			`su -s /bin/sh -c "%s/bin/initdb -D %s/data --encoding=UTF8 --no-locale" %s`,
			pgDir, pgDir, p.siteUser,
		)
		if out, err := runShellW(ctx, w, initCmd); err != nil {
			return fmt.Errorf("postgres: initdb: %w\n%s", err, out)
		}
	}

	// 7. Append devctl-specific settings to postgresql.conf.
	//    - Bind to loopback only (safe dev default).
	//    - Let postgres write logs to stderr (captured by the devctl supervisor).
	fmt.Fprintln(w, "postgres: configuring postgresql.conf...")
	confExtra := "\n# devctl overrides\nlisten_addresses = '127.0.0.1'\nport = 5432\n"
	confPath := filepath.Join(dataDir, "postgresql.conf")
	f, err := os.OpenFile(confPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("postgres: open postgresql.conf: %w", err)
	}
	_, err = f.WriteString(confExtra)
	f.Close()
	if err != nil {
		return fmt.Errorf("postgres: write postgresql.conf: %w", err)
	}

	// 8. Write config.env for the credentials panel.
	//    The default superuser is the OS user postgres was initialised as.
	fmt.Fprintln(w, "postgres: writing config.env...")
	envContent := fmt.Sprintf(
		"DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5432\nDB_USERNAME=%s\nDB_PASSWORD=\nDB_DATABASE=postgres\n",
		p.siteUser,
	)
	if err := os.WriteFile(filepath.Join(pgDir, "config.env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("postgres: write config.env: %w", err)
	}

	fmt.Fprintln(w, "postgres: install complete")
	return nil
}

func (p *PostgresInstaller) Purge(ctx context.Context) error {
	return p.PurgeW(ctx, io.Discard)
}

func (p *PostgresInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	fmt.Fprintln(w, "postgres: WARNING: permanently deleting all PostgreSQL databases and data.")

	// Stop the supervised process before removing files.
	if err := p.supervisor.Stop("postgres"); err != nil {
		fmt.Fprintf(w, "postgres: warning: stop process: %v\n", err)
	}

	// Remove the entire postgres directory (binaries + data + config).
	pgDir := filepath.Join(p.siteHome, "sites", "server", "postgres")
	if err := os.RemoveAll(pgDir); err != nil {
		return fmt.Errorf("postgres: remove dir: %w", err)
	}

	fmt.Fprintln(w, "postgres: purge complete")
	return nil
}

// extractPercona extracts only the subtreePrefix/ entries from a .tar.gz
// archive, stripping that prefix so the contents land directly in destDir.
//
// Example: with subtreePrefix "percona-postgresql18", an entry
// "percona-postgresql18/bin/postgres" is written to destDir/bin/postgres.
// All other top-level directories (percona-pgbouncer, percona-patroni, etc.)
// are silently skipped.
func extractPercona(tarGzPath, subtreePrefix, destDir string) error {
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

	prefix := subtreePrefix + "/"
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		// Normalise paths that begin with "./" (some tar implementations add it).
		name := strings.TrimPrefix(hdr.Name, "./")

		if !strings.HasPrefix(name, prefix) {
			continue // skip other components (pgbouncer, patroni, etc.)
		}

		relPath := strings.TrimPrefix(name, prefix)
		if relPath == "" {
			continue // skip the subtree root dir entry itself
		}

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
			_ = os.Remove(destPath) // remove stale symlink if present
			if err := os.Symlink(hdr.Linkname, destPath); err != nil {
				return fmt.Errorf("symlink %s: %w", destPath, err)
			}
		}
	}
	return nil
}

// wrapOutput wraps an error with a label and the command output for better
// diagnostics in the install log stream.
func wrapOutput(label string, err error, output string) error {
	if output == "" {
		return err
	}
	return &outputError{label: label, err: err, output: output}
}

type outputError struct {
	label  string
	err    error
	output string
}

func (e *outputError) Error() string {
	return e.label + ": " + e.err.Error() + "\n" + e.output
}

func (e *outputError) Unwrap() error { return e.err }
