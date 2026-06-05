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

	"github.com/danielgormly/devctl/paths"
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

	// postgresSuperuser is the default database role for Laravel (.env DB_USERNAME=root).
	postgresSuperuser = "root"
	// postgresDevPassword is the local dev password (required by GUI clients like TablePlus).
	postgresDevPassword = "devctl"
)

// postgresClientBins lists client tools exposed in the shared bin directory.
// Percona's psql is a shell wrapper around psql.bin — it must not be symlinked
// into bin/ because it resolves paths relative to the caller's directory.
var postgresClientBins = []struct {
	name   string
	binary string // filename under postgres/bin/
}{
	{"psql", "psql.bin"},
	{"pg_dump", "pg_dump"},
	{"pg_restore", "pg_restore"},
	{"createdb", "createdb"},
	{"dropdb", "dropdb"},
}

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
// tarball to {serverRoot}/postgres/, initialises the data directory as
// siteUser (PostgreSQL refuses to start as root), and runs postgres as a
// supervised child process of devctl with privilege drop via ManagedUser.
//
// No APT packages for PostgreSQL itself — only libreadline-dev is installed as
// a system library required by the tarball. No systemd unit.
type PostgresInstaller struct {
	supervisor *services.Supervisor
	serverRoot string // absolute path to the devctl server directory
	siteUser   string // username of the non-root site user (e.g. "alice")
}

func (p *PostgresInstaller) ServiceID() string { return "postgres" }

func (p *PostgresInstaller) postgresDir() string {
	return paths.ServiceDir(p.serverRoot, "postgres")
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
	//    Superuser role is "root" (Laravel default). Local socket uses trust;
	//    TCP connections on 127.0.0.1 require scram-sha-256 + postgresDevPassword.
	if !fileExists(filepath.Join(dataDir, "PG_VERSION")) {
		fmt.Fprintln(w, "postgres: initialising data directory...")
		initCmd := fmt.Sprintf(
			`su -s /bin/sh -c "%s/bin/initdb -D %s/data --username=%s --encoding=UTF8 --no-locale --auth-local=trust --auth-host=scram-sha-256" %s`,
			pgDir, pgDir, postgresSuperuser, p.siteUser,
		)
		if out, err := runShellW(ctx, w, initCmd); err != nil {
			return fmt.Errorf("postgres: initdb: %w\n%s", err, out)
		}
		fmt.Fprintln(w, "postgres: setting superuser password...")
		if err := setPostgresPasswordSingleUser(pgDir, p.siteUser); err != nil {
			return fmt.Errorf("postgres: set password: %w", err)
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

	// 8. Write config.env for the credentials panel (no DB_DATABASE — app-specific).
	fmt.Fprintln(w, "postgres: writing config.env...")
	if err := writePostgresConfigEnv(pgDir); err != nil {
		return fmt.Errorf("postgres: write config.env: %w", err)
	}

	// 9. Write client wrappers into the shared bin dir (sets LD_LIBRARY_PATH and
	//    default connection env vars so tools work from PATH like mysql/redis).
	fmt.Fprintln(w, "postgres: writing client binary wrappers...")
	if err := writePostgresClientWrappers(p.serverRoot); err != nil {
		return fmt.Errorf("postgres: client wrappers: %w", err)
	}

	fmt.Fprintln(w, "postgres: install complete")
	return nil
}

// LatestVersion returns ("", nil) — PostgreSQL (Percona distribution) does
// not have a simple upstream API to check for the latest version. Update the
// postgresVersion constant in this file manually when a new release is available.
func (p *PostgresInstaller) LatestVersion(_ context.Context) (string, error) {
	return "", nil
}

// UpdateW stops PostgreSQL, downloads the new Percona tarball, and extracts
// the new binaries over the existing installation. The data directory is
// preserved. The caller (API handler) is responsible for restarting the service.
func (p *PostgresInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	if !p.IsInstalled() {
		return fmt.Errorf("postgres: not installed")
	}

	pgDir := p.postgresDir()
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("percona-postgresql-%s-update.tar.gz", postgresVersion))
	defer os.Remove(tmpTar)

	fmt.Fprintln(w, "postgres: stopping service...")
	if err := p.supervisor.Stop("postgres"); err != nil {
		fmt.Fprintf(w, "postgres: warning: stop: %v\n", err)
	}

	url := perconaTarURL()
	fmt.Fprintf(w, "postgres: downloading Percona PostgreSQL %s...\n", postgresVersion)
	if err := curlDownloadW(ctx, w, url, tmpTar); err != nil {
		return fmt.Errorf("postgres: update download: %w", err)
	}

	fmt.Fprintln(w, "postgres: extracting new binaries (preserving data/)...")
	subtree := fmt.Sprintf("percona-postgresql%s", postgresMajor)
	if err := extractPercona(tmpTar, subtree, pgDir); err != nil {
		return fmt.Errorf("postgres: update extract: %w", err)
	}

	// Re-transfer ownership so the new files are also owned by the site user.
	if p.siteUser != "" {
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", p.siteUser, p.siteUser, pgDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("postgres: update chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "postgres: refreshing client binary wrappers...")
	if err := writePostgresClientWrappers(p.serverRoot); err != nil {
		return fmt.Errorf("postgres: client wrappers: %w", err)
	}

	fmt.Fprintf(w, "postgres: binary replaced with %s\n", postgresVersion)
	return nil
}

func (p *PostgresInstaller) Purge(ctx context.Context) error {
	return p.PurgeW(ctx, io.Discard, false)
}

func (p *PostgresInstaller) PurgeW(ctx context.Context, w io.Writer, preserveData bool) error {
	if preserveData {
		fmt.Fprintln(w, "postgres: WARNING: removing PostgreSQL binaries (data/ will be preserved).")
	} else {
		fmt.Fprintln(w, "postgres: WARNING: permanently deleting all PostgreSQL databases and data.")
	}

	// Stop the supervised process before removing files.
	if err := p.supervisor.Stop("postgres"); err != nil {
		fmt.Fprintf(w, "postgres: warning: stop process: %v\n", err)
	}

	removePostgresClientWrappers(p.serverRoot)

	pgDir := paths.ServiceDir(p.serverRoot, "postgres")
	if preserveData {
		// Remove everything except data/ so the binaries are gone but
		// databases survive.
		fmt.Fprintln(w, "postgres: purging binaries (preserving data/)...")
		if err := removeAllExcept(pgDir, "data"); err != nil {
			return fmt.Errorf("postgres: remove binaries: %w", err)
		}
	} else {
		// Remove the entire postgres directory (binaries + data + config).
		if err := os.RemoveAll(pgDir); err != nil {
			return fmt.Errorf("postgres: remove dir: %w", err)
		}
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

// writePostgresConfigEnv writes the Laravel-ready credentials file for the
// dashboard. DB_DATABASE is intentionally omitted — it is app-specific.
func writePostgresConfigEnv(pgDir string) error {
	envContent := fmt.Sprintf(
		"DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_PORT=5432\nDB_USERNAME=%s\nDB_PASSWORD=%s\n",
		postgresSuperuser,
		postgresDevPassword,
	)
	return os.WriteFile(filepath.Join(pgDir, "config.env"), []byte(envContent), 0600)
}

// postgresClientEnv returns env vars for client wrappers.
func postgresClientEnv(pgDir string) map[string]string {
	return map[string]string{
		"LD_LIBRARY_PATH": filepath.Join(pgDir, "lib"),
		"PGHOST":          "127.0.0.1",
		"PGPORT":          "5432",
		"PGUSER":          postgresSuperuser,
		"PGPASSWORD":      postgresDevPassword,
		"PGDATABASE":      "postgres",
	}
}

// writePostgresClientWrappers installs executable wrappers in the shared bin dir.
func writePostgresClientWrappers(serverRoot string) error {
	pgDir := paths.ServiceDir(serverRoot, "postgres")
	binDir := paths.BinDir(serverRoot)
	env := postgresClientEnv(pgDir)
	for _, c := range postgresClientBins {
		target := filepath.Join(pgDir, "bin", c.binary)
		if !fileExists(target) {
			continue
		}
		if err := WrapperScriptIntoBinDir(binDir, c.name, target, env); err != nil {
			return err
		}
	}
	return nil
}

func removePostgresClientWrappers(serverRoot string) {
	binDir := paths.BinDir(serverRoot)
	for _, c := range postgresClientBins {
		UnlinkFromBinDir(binDir, c.name)
	}
}

// setPostgresPasswordSingleUser sets the superuser password without starting the
// server, using postgres --single mode and local trust auth.
func setPostgresPasswordSingleUser(pgDir, siteUser string) error {
	sql := fmt.Sprintf(`ALTER USER %s WITH PASSWORD '%s';`, postgresSuperuser, postgresDevPassword)
	return runPostgresSingleUserSQL(pgDir, siteUser, sql)
}

// EnsurePostgresConfig migrates existing installations: refreshes config.env and
// client wrappers, renames the legacy site-user superuser to root, and ensures
// the dev password is set. Safe to call on every devctl startup.
func EnsurePostgresConfig(serverRoot, siteUser string) error {
	pgDir := paths.ServiceDir(serverRoot, "postgres")
	if !fileExists(filepath.Join(pgDir, "bin", "postgres")) {
		return nil
	}
	if err := writePostgresConfigEnv(pgDir); err != nil {
		return err
	}
	if err := writePostgresClientWrappers(serverRoot); err != nil {
		return err
	}
	if !fileExists(filepath.Join(pgDir, "data", "PG_VERSION")) {
		return nil
	}
	return ensurePostgresRoleAndPassword(pgDir, siteUser)
}

// postgresMigrateSQL returns SQL to ensure the root superuser exists with the dev password.
// Legacy clusters initialised as the site user cannot rename that role in --single mode
// (session user cannot be renamed), so we create root alongside it.
func postgresMigrateSQL(siteUser string) string {
	_ = siteUser // reserved for future cleanup of legacy roles
	return fmt.Sprintf(
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE "%s" WITH LOGIN SUPERUSER PASSWORD '%s'; ELSE ALTER ROLE "%s" WITH PASSWORD '%s'; END IF; END $$;`,
		postgresSuperuser, postgresSuperuser, postgresDevPassword, postgresSuperuser, postgresDevPassword,
	)
}

// runPostgresSingleUserSQL runs SQL against a stopped cluster via postgres --single.
func runPostgresSingleUserSQL(pgDir, siteUser, sql string) error {
	cmd := fmt.Sprintf(
		`printf '%%s\n' %q | su -s /bin/sh -c 'LD_LIBRARY_PATH=%q/lib %q/bin/postgres --single -D %q/data postgres' %q`,
		sql, pgDir, pgDir, pgDir, siteUser,
	)
	if out, err := runShell(context.Background(), cmd); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// ensurePostgresRoleAndPassword renames a legacy site-user superuser to root and
// sets the dev password. Uses the cluster's Unix socket when running; falls back
// to --single mode when the server is stopped (e.g. port 5432 already taken).
func ensurePostgresRoleAndPassword(pgDir, siteUser string) error {
	dataDir := filepath.Join(pgDir, "data")
	migrateSQL := postgresMigrateSQL(siteUser)

	readyCmd := fmt.Sprintf("LD_LIBRARY_PATH=%s/lib %s/bin/pg_isready -h %s -p 5432 -q", pgDir, pgDir, dataDir)
	if _, err := runShell(context.Background(), readyCmd); err != nil {
		return runPostgresSingleUserSQL(pgDir, siteUser, migrateSQL)
	}

	connectUser := postgresSuperuser
	tryRoot := fmt.Sprintf(
		`su -s /bin/sh -c 'LD_LIBRARY_PATH=%q/lib %q/bin/psql.bin -h %q -U %q -d postgres -tAc "SELECT 1"' %q`,
		pgDir, pgDir, dataDir, postgresSuperuser, siteUser,
	)
	if _, err := runShell(context.Background(), tryRoot); err != nil && siteUser != "" && siteUser != postgresSuperuser {
		connectUser = siteUser
	}

	cmd := fmt.Sprintf(
		`su -s /bin/sh -c 'LD_LIBRARY_PATH=%q/lib %q/bin/psql.bin -h %q -U %q -d postgres -c %q' %q`,
		pgDir, pgDir, dataDir, connectUser, migrateSQL, siteUser,
	)
	if out, err := runShell(context.Background(), cmd); err != nil {
		return fmt.Errorf("postgres: migrate role/password: %w\n%s", err, out)
	}
	return nil
}
