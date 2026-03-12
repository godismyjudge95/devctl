package install

import (
	"context"
	"io"
	"os"
)

// PostgresInstaller installs PostgreSQL from the official PGDG APT repository.
// Ref: https://www.postgresql.org/download/linux/ubuntu/
//
// We install the latest stable version (currently 18). The setup script
// /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh handles adding the
// signed APT source automatically.
type PostgresInstaller struct{}

func (p *PostgresInstaller) ServiceID() string { return "postgres" }

func (p *PostgresInstaller) IsInstalled() bool { return fileExists("/usr/bin/psql") }

func (p *PostgresInstaller) Install(ctx context.Context) error {
	return p.InstallW(ctx, io.Discard)
}

func (p *PostgresInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if p.IsInstalled() {
		return nil
	}

	// Install the helper package that ships the PGDG setup script.
	if err := aptInstallW(ctx, w, "postgresql-common"); err != nil {
		return err
	}

	// Run the non-interactive setup script — it adds the PGDG keyring and
	// APT source, then runs apt-get update automatically.
	out, err := runShellW(ctx, w, "echo 'y' | /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh")
	if err != nil {
		return wrapOutput("pgdg setup script", err, out)
	}

	if err := aptInstallW(ctx, w, "postgresql-18"); err != nil {
		return err
	}

	return enableAndStartW(ctx, w, "postgresql")
}

func (p *PostgresInstaller) Purge(ctx context.Context) error {
	return p.PurgeW(ctx, io.Discard)
}

func (p *PostgresInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	stopAndDisableW(ctx, w, "postgresql")
	if err := aptPurgeW(ctx, w, "postgresql*", "libpq-dev"); err != nil {
		return err
	}
	removeFiles(
		"/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc",
		"/etc/apt/sources.list.d/pgdg.list",
	)
	_ = os.RemoveAll("/etc/postgresql")
	_ = os.RemoveAll("/var/lib/postgresql")
	return nil
}

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
