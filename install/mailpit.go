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
	siteUser   string
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

	// 5. Write config.env with MP_* env vars and Laravel connection info.
	fmt.Fprintln(w, "mailpit: writing config.env...")
	envContent := "MAIL_MAILER=smtp\nMAIL_HOST=127.0.0.1\nMAIL_PORT=1025\n" +
		"MP_SMTP_BIND_ADDR=127.0.0.1:1025\n" +
		"MP_UI_BIND_ADDR=127.0.0.1:8025\n" +
		"MP_DATABASE=./data/mailpit.db\n" +
		"MP_MAX_MESSAGES=500\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("mailpit: write config.env: %w", err)
	}

	// 6. Transfer ownership to the site user.
	if m.siteUser != "" {
		fmt.Fprintf(w, "mailpit: chowning %s to %s...\n", mailpitDir, m.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", m.siteUser, m.siteUser, mailpitDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("mailpit: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "mailpit: install complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest Mailpit version.
func (m *MailpitInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchGitHubLatestVersion(ctx, "axllent/mailpit")
}

// UpdateW stops Mailpit, replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (m *MailpitInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := m.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("mailpit: update: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/axllent/mailpit/releases/download/%s/mailpit-linux-amd64.tar.gz", latest)

	mailpitDir := paths.ServiceDir(m.serverRoot, "mailpit")
	binPath := filepath.Join(mailpitDir, "mailpit")
	tmpTar := filepath.Join(os.TempDir(), "mailpit-update-linux-amd64.tar.gz")
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "mailpit: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("mailpit: update download: %w", err)
	}

	fmt.Fprintln(w, "mailpit: stopping mailpit...")
	if err := m.supervisor.Stop("mailpit"); err != nil {
		fmt.Fprintf(w, "mailpit: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "mailpit: replacing binary...")
	if err := extractFromTar(tmpTar, "mailpit", binPath); err != nil {
		return fmt.Errorf("mailpit: update extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("mailpit: update chmod: %w", err)
	}

	fmt.Fprintf(w, "mailpit: binary replaced with %s\n", latest)
	return nil
}

func (m *MailpitInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard, false)
}

func (m *MailpitInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
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
