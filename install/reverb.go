package install

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

// ReverbInstaller sets up a fresh Laravel app at {serverRoot}/reverb,
// configures it as a Reverb broadcasting server, and registers a WS
// vhost at reverb.test via Caddy.
type ReverbInstaller struct {
	siteManager *sites.Manager
	queries     *dbq.Queries
	supervisor  *services.Supervisor
	siteUser    string // non-root OS user who owns ~/sites (e.g. "alice")
	siteHome    string // home directory of siteUser (e.g. "/home/alice") — used for runAsUserW and composer bin path
	serverRoot  string // absolute path to the devctl server directory (e.g. "/home/alice/ddev/sites/server")
}

func (r *ReverbInstaller) ServiceID() string { return "reverb" }

func (r *ReverbInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(r.serverRoot, "reverb"), "artisan"))
}

func (r *ReverbInstaller) Install(ctx context.Context) error {
	return r.InstallW(ctx, io.Discard)
}

// InstallW provisions the Reverb Laravel app and Caddy vhost, streaming
// command output to w.
func (r *ReverbInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if r.IsInstalled() {
		fmt.Fprintln(w, "reverb: already installed")
		return nil
	}

	sitesDir := paths.ServerDir(r.serverRoot)
	reverbDir := paths.ServiceDir(r.serverRoot, "reverb")

	// 1. Ensure $HOME/sites exists (owned by siteUser).
	if err := os.MkdirAll(sitesDir, 0755); err != nil {
		return fmt.Errorf("reverb: create sites dir: %w", err)
	}

	// 2. Create a new Laravel project (must run as siteUser so files are
	//    owned correctly). Run in sitesDir with "reverb" as the name —
	//    laravel new treats <name> as relative to cwd, not as an absolute path.
	laravelBin := filepath.Join(r.siteHome, ".config", "composer", "vendor", "bin", "laravel")
	fmt.Fprintln(w, "reverb: creating Laravel project...")
	_, err := runAsUserW(ctx, w, r.siteUser, r.siteHome, sitesDir,
		fmt.Sprintf("%s new reverb --no-interaction --database=sqlite", laravelBin))
	if err != nil {
		return fmt.Errorf("laravel new: %w", err)
	}

	// 3. Install broadcasting (sets BROADCAST_CONNECTION=reverb, writes
	//    REVERB_APP_ID/KEY/SECRET to .env).
	fmt.Fprintln(w, "reverb: installing broadcasting...")
	_, err = runAsUserW(ctx, w, r.siteUser, r.siteHome, reverbDir,
		"php artisan install:broadcasting --reverb --without-node --no-interaction")
	if err != nil {
		return fmt.Errorf("install:broadcasting: %w", err)
	}

	// 4. Patch .env with correct server/host settings.
	fmt.Fprintln(w, "reverb: patching .env...")
	envPatches := map[string]string{
		"REVERB_SERVER_HOST": "127.0.0.1",
		"REVERB_SERVER_PORT": "7383",
		"REVERB_HOST":        "reverb.test",
		"REVERB_PORT":        "443",
		"REVERB_SCHEME":      "https",
	}
	if err := patchEnvFile(filepath.Join(reverbDir, ".env"), envPatches); err != nil {
		return fmt.Errorf("reverb: patch .env: %w", err)
	}

	// 5. Patch config/reverb.php — allow all origins.
	fmt.Fprintln(w, "reverb: patching allowed_origins...")
	if err := patchReverbAllowedOrigins(filepath.Join(reverbDir, "config", "reverb.php")); err != nil {
		// Non-fatal: log and continue.
		fmt.Fprintf(w, "reverb: warning: patch allowed_origins: %v\n", err)
	}

	// 6. Register the vhost via the site manager.
	fmt.Fprintln(w, "reverb: creating reverb.test site...")
	_, err = r.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:       "reverb.test",
		RootPath:     reverbDir,
		SiteType:     "ws",
		WSUpstream:   "127.0.0.1:7383",
		HTTPS:        true,
		ServiceVhost: true,
	})
	if err != nil {
		// Best-effort: site may already exist.
		fmt.Fprintf(w, "reverb: warning: create site: %v\n", err)
	}

	fmt.Fprintln(w, "reverb: install complete")
	return nil
}

func (r *ReverbInstaller) Purge(ctx context.Context) error {
	return r.PurgeW(ctx, io.Discard)
}

// PurgeW stops the supervised process, removes the Caddy vhost and the
// directory.
func (r *ReverbInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := r.supervisor.Stop("reverb"); err != nil {
		fmt.Fprintf(w, "reverb: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhost and DB row.
	if r.siteManager != nil {
		if err := r.siteManager.Delete(ctx, "reverb-test"); err != nil {
			fmt.Fprintf(w, "reverb: warning: delete site: %v\n", err)
		}
	}

	// Remove the directory.
	reverbDir := paths.ServiceDir(r.serverRoot, "reverb")
	if err := os.RemoveAll(reverbDir); err != nil {
		return fmt.Errorf("reverb: remove dir: %w", err)
	}

	fmt.Fprintln(w, "reverb: purge complete")
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// runAsUserW runs a shell command as the given OS user via `sudo -u <user>`.
// dir is the working directory (empty = default). Output is streamed to w
// and also returned as a string.
func runAsUserW(ctx context.Context, w io.Writer, username, home, dir, command string) (string, error) {
	var shellCmd string
	if dir != "" {
		shellCmd = fmt.Sprintf("cd '%s' && %s", dir, command)
	} else {
		shellCmd = command
	}
	cmd := exec.CommandContext(ctx, "sudo", "-u", username, "--", "sh", "-c", shellCmd)
	// Provide a minimal but correct environment: HOME must point to the
	// siteUser's home so that tools like composer and npm resolve ~ correctly.
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USER="+username,
	)
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	err := cmd.Run()
	return buf.String(), err
}

// patchEnvFile upserts key=value pairs in a .env file.
// Existing keys are updated in-place; missing keys are appended.
func patchEnvFile(path string, patches map[string]string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	remaining := make(map[string]string, len(patches))
	for k, v := range patches {
		remaining[k] = v
	}

	for i, line := range lines {
		for k, v := range remaining {
			if strings.HasPrefix(line, k+"=") || strings.HasPrefix(line, k+" =") {
				lines[i] = k + "=" + v
				delete(remaining, k)
				break
			}
		}
	}

	// Append any keys that were not found.
	for k, v := range remaining {
		lines = append(lines, k+"="+v)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// patchReverbAllowedOrigins replaces the allowed_origins array in
// config/reverb.php with a wildcard ['*'].
func patchReverbAllowedOrigins(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`'allowed_origins'\s*=>\s*\[[^\]]*\]`)
	patched := re.ReplaceAll(content, []byte(`'allowed_origins' => ['*']`))
	if string(patched) == string(content) {
		// Pattern not found — scan line by line for a looser match.
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		var sb strings.Builder
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			if !found && strings.Contains(line, "allowed_origins") {
				sb.WriteString("            'allowed_origins' => ['*'],\n")
				found = true
			} else {
				sb.WriteString(line + "\n")
			}
		}
		if found {
			patched = []byte(sb.String())
		}
	}

	return os.WriteFile(path, patched, 0644)
}
