package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

const (
	mailpitVersion = "v1.29.2"
	mailpitURL     = "https://github.com/axllent/mailpit/releases/download/" + mailpitVersion + "/mailpit-linux-amd64.tar.gz"
)

// MailpitInstaller downloads the Mailpit binary to
// {serverRoot}/mailpit/ and writes config.env with Laravel .env keys.
type MailpitInstaller struct {
	supervisor *services.Supervisor
	serverRoot string // absolute path to the devctl server directory
}

func (m *MailpitInstaller) ServiceID() string { return "mailpit" }

func (m *MailpitInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(m.serverRoot, "mailpit"), "mailpit"))
}

func (m *MailpitInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MailpitInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "mailpit: already installed")
		return nil
	}

	mailpitDir := paths.ServiceDir(m.serverRoot, "mailpit")
	binPath := filepath.Join(mailpitDir, "mailpit")
	dataDir := filepath.Join(mailpitDir, "data")
	envPath := filepath.Join(mailpitDir, "config.env")
	tmpTar := filepath.Join(os.TempDir(), "mailpit-linux-amd64.tar.gz")
	defer os.Remove(tmpTar)

	// 1. Create directories.
	fmt.Fprintln(w, "mailpit: creating directory...")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("mailpit: create dir: %w", err)
	}

	// 2. Download tarball.
	fmt.Fprintf(w, "mailpit: downloading %s...\n", mailpitVersion)
	if err := curlDownloadW(ctx, w, mailpitURL, tmpTar); err != nil {
		return fmt.Errorf("mailpit: download: %w", err)
	}

	// 3. Extract mailpit binary from tarball (binary is at root of tarball).
	fmt.Fprintln(w, "mailpit: extracting binary...")
	if err := extractFromTar(tmpTar, "mailpit", binPath); err != nil {
		return fmt.Errorf("mailpit: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("mailpit: chmod binary: %w", err)
	}

	// 4. Symlink into the shared bin dir so mailpit is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(m.serverRoot), "mailpit", binPath); err != nil {
		fmt.Fprintf(w, "mailpit: warning: %v\n", err)
	}

	// 5. Write config.env with connection info for Laravel .env.
	fmt.Fprintln(w, "mailpit: writing config.env...")
	envContent := "MAIL_MAILER=smtp\nMAIL_HOST=127.0.0.1\nMAIL_PORT=1025\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("mailpit: write config.env: %w", err)
	}

	fmt.Fprintln(w, "mailpit: install complete")
	return nil
}

func (m *MailpitInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard)
}

func (m *MailpitInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := m.supervisor.Stop("mailpit"); err != nil {
		fmt.Fprintf(w, "mailpit: warning: stop process: %v\n", err)
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(m.serverRoot), "mailpit")

	// Remove the directory (binary + data + config.env).
	mailpitDir := paths.ServiceDir(m.serverRoot, "mailpit")
	if err := os.RemoveAll(mailpitDir); err != nil {
		return fmt.Errorf("mailpit: remove dir: %w", err)
	}

	fmt.Fprintln(w, "mailpit: purge complete")
	return nil
}
