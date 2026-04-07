package install

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

//go:embed meilisearch-config.toml
var meilisearchConfigTemplate []byte

// MeilisearchInstaller downloads the Meilisearch binary to
// {serverRoot}/meilisearch/, generates a master key, writes
// config.env, and registers a Caddy reverse-proxy vhost at meilisearch.test.
type MeilisearchInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string // absolute path to the devctl server directory
	siteUser    string
}

func (m *MeilisearchInstaller) ServiceID() string { return "meilisearch" }

func (m *MeilisearchInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(m.serverRoot, "meilisearch"), "meilisearch"))
}

func (m *MeilisearchInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MeilisearchInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "meilisearch: already installed")
		return nil
	}

	latest, err := m.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("meilisearch: resolve latest version: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/meilisearch/meilisearch/releases/download/%s/meilisearch-linux-amd64", latest)

	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	binPath := filepath.Join(meiliDir, "meilisearch")
	envPath := filepath.Join(meiliDir, "config.env")

	// 1. Create directory.
	fmt.Fprintln(w, "meilisearch: creating directory...")
	if err := os.MkdirAll(meiliDir, 0755); err != nil {
		return fmt.Errorf("meilisearch: create dir: %w", err)
	}

	// 2. Download binary.
	fmt.Fprintf(w, "meilisearch: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, binPath); err != nil {
		return fmt.Errorf("meilisearch: download: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("meilisearch: chmod binary: %w", err)
	}

	// 3. Symlink into the shared bin dir so meilisearch is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(m.serverRoot), "meilisearch", binPath); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: %v\n", err)
	}

	// 4. Use a fixed, easily-typeable master key (mirrors Laravel Herd's approach).
	key := "DEVCTL"

	// 5. Write config.env with Laravel Scout connection info.
	fmt.Fprintln(w, "meilisearch: writing config.env...")
	envContent := fmt.Sprintf("MEILISEARCH_KEY=%s\nMEILISEARCH_HOST=https://meilisearch.test\n", key)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("meilisearch: write config.env: %w", err)
	}

	// 6. Write config.toml with full defaults.
	fmt.Fprintln(w, "meilisearch: writing config.toml...")
	if err := writeMeilisearchConf(meiliDir, key); err != nil {
		return fmt.Errorf("meilisearch: write config.toml: %w", err)
	}

	// 7. Register Caddy reverse-proxy vhost at meilisearch.test.
	fmt.Fprintln(w, "meilisearch: creating meilisearch.test Caddy vhost...")
	_, err = m.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:       "meilisearch.test",
		SiteType:     "ws", // reverse_proxy handler — works for plain HTTP too
		WSUpstream:   "127.0.0.1:7700",
		HTTPS:        true,
		ServiceVhost: true,
	})
	if err != nil {
		// Best-effort: vhost may already exist.
		fmt.Fprintf(w, "meilisearch: warning: create site: %v\n", err)
	}

	// 6. Transfer ownership to the site user.
	if m.siteUser != "" {
		fmt.Fprintf(w, "meilisearch: chowning %s to %s...\n", meiliDir, m.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", m.siteUser, m.siteUser, meiliDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("meilisearch: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "meilisearch: install complete")
	return nil
}

func (m *MeilisearchInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard, false)
}

func (m *MeilisearchInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := m.supervisor.Stop("meilisearch"); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhost and DB row.
	if m.siteManager != nil {
		if err := m.siteManager.Delete(ctx, "meilisearch-test"); err != nil {
			fmt.Fprintf(w, "meilisearch: warning: delete site: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(m.serverRoot), "meilisearch")

	// Remove the directory.
	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	if err := os.RemoveAll(meiliDir); err != nil {
		return fmt.Errorf("meilisearch: remove dir: %w", err)
	}

	fmt.Fprintln(w, "meilisearch: purge complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest Meilisearch version.
// If the context carries a pre-resolved version (via install.WithPreResolvedVersion),
// that value is returned immediately without hitting GitHub.
func (m *MeilisearchInstaller) LatestVersion(ctx context.Context) (string, error) {
	if v := preResolvedVersionFromCtx(ctx); v != "" {
		return v, nil
	}
	return fetchGitHubLatestVersion(ctx, "meilisearch/meilisearch")
}

// UpdateW performs a Meilisearch dumpless upgrade (available from v1.12+ → v1.13+):
//  1. Take a snapshot as a safety backup (recommended for experimental features).
//  2. Download the new binary.
//  3. Stop the running process.
//  4. Replace the binary.
//  5. Start with --experimental-dumpless-upgrade; poll until UpgradeDatabase task succeeds.
//  6. Stop the upgrade-mode process; the caller (API handler) restarts normally via supervisor.
func (m *MeilisearchInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := m.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("meilisearch: update: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/meilisearch/meilisearch/releases/download/%s/meilisearch-linux-amd64", latest)

	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	binPath := filepath.Join(meiliDir, "meilisearch")
	masterKey := readEnvKey(filepath.Join(meiliDir, "config.env"), "MEILISEARCH_KEY")

	// ---------- Step 1: snapshot as safety backup ----------
	// Dumpless upgrade is experimental; a snapshot lets us roll back if it goes wrong.
	fmt.Fprintln(w, "meilisearch: creating snapshot backup...")
	if snapErr := meilisearchTriggerSnapshot(ctx, w, masterKey); snapErr != nil {
		fmt.Fprintf(w, "meilisearch: warning: snapshot failed (%v), continuing\n", snapErr)
	}

	// ---------- Step 2: download new binary ----------
	fmt.Fprintf(w, "meilisearch: downloading %s...\n", latest)
	tmpBin := filepath.Join(os.TempDir(), "meilisearch-update-linux-amd64")
	defer os.Remove(tmpBin)
	if err := curlDownloadW(ctx, w, dlURL, tmpBin); err != nil {
		return fmt.Errorf("meilisearch: update download: %w", err)
	}

	// ---------- Step 3: stop the running process ----------
	fmt.Fprintln(w, "meilisearch: stopping meilisearch...")
	if err := m.supervisor.Stop("meilisearch"); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: stop: %v\n", err)
	}

	// ---------- Step 4: replace binary ----------
	fmt.Fprintln(w, "meilisearch: replacing binary...")
	if err := os.Chmod(tmpBin, 0755); err != nil {
		return fmt.Errorf("meilisearch: chmod new binary: %w", err)
	}
	if err := os.Rename(tmpBin, binPath); err != nil {
		// Rename may fail across filesystems — fall back to copy.
		if copyErr := copyFile(tmpBin, binPath); copyErr != nil {
			return fmt.Errorf("meilisearch: replace binary: rename failed (%v), copy also failed: %w", err, copyErr)
		}
		if err := os.Chmod(binPath, 0755); err != nil {
			return fmt.Errorf("meilisearch: chmod binary after copy: %w", err)
		}
	}

	// ---------- Step 5: run with --experimental-dumpless-upgrade ----------
	fmt.Fprintln(w, "meilisearch: launching dumpless upgrade...")
	upgradeCmd := exec.CommandContext(ctx, binPath,
		"--config-file-path", filepath.Join(meiliDir, "config.toml"),
		"--experimental-dumpless-upgrade",
	)
	upgradeCmd.Dir = meiliDir
	upgradeCmd.Stdout = w
	upgradeCmd.Stderr = w
	if err := upgradeCmd.Start(); err != nil {
		return fmt.Errorf("meilisearch: start upgrade process: %w", err)
	}

	// Wait for the UpgradeDatabase task to complete, then stop the process.
	upgradeErr := meilisearchWaitForUpgrade(ctx, w, masterKey)
	_ = upgradeCmd.Process.Kill()
	_ = upgradeCmd.Wait()

	if upgradeErr != nil {
		return fmt.Errorf("meilisearch: dumpless upgrade failed: %w", upgradeErr)
	}

	fmt.Fprintf(w, "meilisearch: upgraded to %s successfully\n", latest)
	return nil
}

// meilisearchTriggerSnapshot calls POST /snapshots as a safety backup before
// a dumpless upgrade. Non-fatal if it fails.
func meilisearchTriggerSnapshot(ctx context.Context, w io.Writer, masterKey string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:7700/snapshots", nil)
	if err != nil {
		return err
	}
	if masterKey != "" {
		req.Header.Set("Authorization", "Bearer "+masterKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("POST /snapshots returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		TaskUID int `json:"taskUid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode snapshot response: %w", err)
	}
	fmt.Fprintf(w, "meilisearch: snapshot task %d enqueued\n", result.TaskUID)
	return nil
}

// meilisearchWaitForUpgrade waits for Meilisearch to start after being launched
// with --experimental-dumpless-upgrade, then polls until the UpgradeDatabase
// task succeeds or fails.
func meilisearchWaitForUpgrade(ctx context.Context, w io.Writer, masterKey string) error {
	if err := meilisearchWaitReady(ctx, w, masterKey, 60*time.Second); err != nil {
		return fmt.Errorf("meilisearch did not become ready: %w", err)
	}

	taskUID, err := meilisearchFindUpgradeTask(ctx, masterKey)
	if err != nil {
		return fmt.Errorf("find UpgradeDatabase task: %w", err)
	}
	fmt.Fprintf(w, "meilisearch: UpgradeDatabase task %d found, waiting for completion...\n", taskUID)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(10 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			if t.After(deadline) {
				return fmt.Errorf("UpgradeDatabase task timed out")
			}
			status, err := meilisearchTaskStatus(ctx, masterKey, taskUID)
			if err != nil {
				fmt.Fprintf(w, "meilisearch: poll error: %v\n", err)
				continue
			}
			fmt.Fprintf(w, "meilisearch: upgrade status: %s\n", status)
			switch status {
			case "succeeded":
				return nil
			case "failed", "canceled":
				return fmt.Errorf("UpgradeDatabase task %d %s", taskUID, status)
			}
		}
	}
}

// meilisearchFindUpgradeTask queries GET /tasks?types=upgradeDatabase and
// returns the UID of the most recent UpgradeDatabase task.
func meilisearchFindUpgradeTask(ctx context.Context, masterKey string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:7700/tasks?types=upgradeDatabase", nil)
	if err != nil {
		return 0, err
	}
	if masterKey != "" {
		req.Header.Set("Authorization", "Bearer "+masterKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var result struct {
		Results []struct {
			UID int `json:"uid"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode tasks response: %w", err)
	}
	if len(result.Results) == 0 {
		return 0, fmt.Errorf("no upgradeDatabase task found")
	}
	return result.Results[0].UID, nil
}

// meilisearchTaskStatus returns the status string for a given task UID.
func meilisearchTaskStatus(ctx context.Context, masterKey string, taskUID int) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:7700/tasks/%d", taskUID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	if masterKey != "" {
		req.Header.Set("Authorization", "Bearer "+masterKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var task struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return "", err
	}
	return task.Status, nil
}

// meilisearchWaitReady polls GET /health until Meilisearch reports available status.
func meilisearchWaitReady(ctx context.Context, w io.Writer, masterKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			if t.After(deadline) {
				return fmt.Errorf("timed out waiting for meilisearch to be ready")
			}
			freshReq, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:7700/health", nil)
			if masterKey != "" {
				freshReq.Header.Set("Authorization", "Bearer "+masterKey)
			}
			resp, err := http.DefaultClient.Do(freshReq)
			if err != nil {
				fmt.Fprintf(w, "meilisearch: health check: %v\n", err)
				continue
			}
			var health struct {
				Status string `json:"status"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&health)
			resp.Body.Close()
			if health.Status == "available" {
				return nil
			}
			fmt.Fprintf(w, "meilisearch: health: %s\n", health.Status)
		}
	}
}

// EnsureMeilisearchConf writes config.toml to the Meilisearch service directory
// if the file is missing. Reads the master key from the existing config.env so
// it matches the already-provisioned key. Safe to call on every startup — it is
// a no-op when config.toml already exists.
func EnsureMeilisearchConf(serverRoot string) error {
	meiliDir := paths.ServiceDir(serverRoot, "meilisearch")
	// Read master key from config.env (written at install time).
	key := readEnvKey(filepath.Join(meiliDir, "config.env"), "MEILISEARCH_KEY")
	return writeMeilisearchConf(meiliDir, key)
}

// readEnvKey reads a KEY=VALUE line from an env file, returning the value for
// the given key. Returns an empty string if the file or key is not found.
func readEnvKey(envPath, key string) string {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

// generateRandomHex returns a random hex string of n bytes (length 2*n).
func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// writeMeilisearchConf writes config.toml to dir/config.toml using the official
// Meilisearch v1.37.0 default config as a base, stamping in devctl-specific values.
// The file is only written if it does not yet exist so user edits are preserved.
func writeMeilisearchConf(dir, masterKey string) error {
	confPath := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(confPath); err == nil {
		return nil // already exists — don't overwrite
	}

	// Overrides to apply to the official template.
	// TOML lines are of the form: key = value (with optional comments)
	overrides := map[string]string{
		"http_addr":    `"127.0.0.1:7700"`,
		"master_key":   fmt.Sprintf("%q", masterKey),
		"no_analytics": "true",
		"env":          `"development"`,
	}

	lines := strings.Split(string(meilisearchConfigTemplate), "\n")
	applied := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip blank lines.
		if trimmed == "" {
			continue
		}
		// Match both active and commented-out keys: `key = ...` or `# key = ...`
		// TrimLeft strips leading '#' and space chars so both forms reduce to `key = value`.
		active := strings.TrimLeft(trimmed, "# ")
		parts := strings.SplitN(active, "=", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if newVal, ok := overrides[key]; ok && !applied[key] {
			lines[i] = key + " = " + newVal
			applied[key] = true
		}
	}

	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
}
