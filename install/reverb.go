package install

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/internal/runuser"
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
	siteHome    string // home directory of siteUser (e.g. "/home/alice") — used as HOME for runAsUserW
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

	// 2. Create a new Laravel project using composer create-project (must run
	//    as siteUser so files are owned correctly). Run in sitesDir with
	//    "reverb" as the project name — composer treats it as relative to cwd.
	//    We use composer directly rather than the `laravel` CLI installer
	//    because the Laravel global installer is not guaranteed to be present;
	//    composer IS always available at {serverRoot}/bin/composer.
	composerBin := filepath.Join(paths.BinDir(r.serverRoot), "composer")
	fmt.Fprintln(w, "reverb: creating Laravel project...")
	_, err := runuser.RunAsUserW(ctx, w, r.siteUser, r.siteHome, sitesDir,
		fmt.Sprintf("%s create-project laravel/laravel reverb --no-interaction --prefer-dist", composerBin))
	if err != nil {
		return fmt.Errorf("composer create-project: %w", err)
	}

	// 3. Install broadcasting (sets BROADCAST_CONNECTION=reverb, writes
	//    REVERB_APP_ID/KEY/SECRET to .env).
	fmt.Fprintln(w, "reverb: installing broadcasting...")
	_, err = runuser.RunAsUserW(ctx, w, r.siteUser, r.siteHome, reverbDir,
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

// LatestVersion queries Packagist for the latest stable laravel/reverb release.
func (r *ReverbInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchPackagistLatestVersion(ctx, "laravel/reverb")
}

// UpdateW runs `composer update laravel/reverb` inside the Reverb app directory
// to pull the latest version. The supervisor is stopped before updating and the
// caller (API handler) is responsible for restarting the service.
func (r *ReverbInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	if !r.IsInstalled() {
		return fmt.Errorf("reverb: not installed")
	}

	reverbDir := paths.ServiceDir(r.serverRoot, "reverb")

	fmt.Fprintln(w, "reverb: stopping service...")
	if err := r.supervisor.Stop("reverb"); err != nil {
		fmt.Fprintf(w, "reverb: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "reverb: running composer update laravel/reverb...")
	_, err := runuser.RunAsUserW(ctx, w, r.siteUser, r.siteHome, reverbDir,
		"composer update laravel/reverb --no-interaction --prefer-dist")
	if err != nil {
		return fmt.Errorf("reverb: composer update: %w", err)
	}

	fmt.Fprintln(w, "reverb: update complete")
	return nil
}

func (r *ReverbInstaller) Purge(ctx context.Context) error {
	return r.PurgeW(ctx, io.Discard, false)
}

// PurgeW stops the supervised process, removes the Caddy vhost and the
// directory.
func (r *ReverbInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
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
