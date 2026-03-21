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

const (
	meilisearchVersion = "v1.37.0"
	meilisearchURL     = "https://github.com/meilisearch/meilisearch/releases/download/" + meilisearchVersion + "/meilisearch-linux-amd64"
)

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

	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	binPath := filepath.Join(meiliDir, "meilisearch")
	envPath := filepath.Join(meiliDir, "config.env")

	// 1. Create directory.
	fmt.Fprintln(w, "meilisearch: creating directory...")
	if err := os.MkdirAll(meiliDir, 0755); err != nil {
		return fmt.Errorf("meilisearch: create dir: %w", err)
	}

	// 2. Download binary.
	fmt.Fprintf(w, "meilisearch: downloading %s...\n", meilisearchVersion)
	if err := curlDownloadW(ctx, w, meilisearchURL, binPath); err != nil {
		return fmt.Errorf("meilisearch: download: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("meilisearch: chmod binary: %w", err)
	}

	// 3. Symlink into the shared bin dir so meilisearch is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(m.serverRoot), "meilisearch", binPath); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: %v\n", err)
	}

	// 4. Generate a random 32-byte hex master key.
	key, err := generateRandomHex(32)
	if err != nil {
		return fmt.Errorf("meilisearch: generate master key: %w", err)
	}

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
		Domain:     "meilisearch.test",
		SiteType:   "ws", // reverse_proxy handler — works for plain HTTP too
		WSUpstream: "127.0.0.1:7700",
		HTTPS:      true,
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
func (m *MeilisearchInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchGitHubLatestVersion(ctx, "meilisearch/meilisearch")
}

// UpdateW performs an autonomous Meilisearch update:
//  1. Trigger a dump via the Meilisearch API and wait for it to complete.
//  2. Stop the running process.
//  3. Replace the binary with the latest version.
//  4. Start with --import-dump to import the dump, blocking until import is done.
//  5. Stop the import-mode process; the caller (API handler) restarts normally.
func (m *MeilisearchInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := m.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("meilisearch: update: %w", err)
	}
	dlURL := fmt.Sprintf("https://github.com/meilisearch/meilisearch/releases/download/%s/meilisearch-linux-amd64", latest)

	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	binPath := filepath.Join(meiliDir, "meilisearch")

	// Read the master key from config.env so we can authenticate API calls.
	masterKey := readEnvKey(filepath.Join(meiliDir, "config.env"), "MEILISEARCH_KEY")

	// ---------- Step 1: create a dump ----------
	fmt.Fprintln(w, "meilisearch: creating dump...")
	dumpUID, err := meilisearchTriggerDump(ctx, masterKey)
	if err != nil {
		return fmt.Errorf("meilisearch: trigger dump: %w", err)
	}
	fmt.Fprintf(w, "meilisearch: dump task %s started, waiting for completion...\n", dumpUID)
	dumpFile, err := meilisearchWaitForDump(ctx, w, masterKey, dumpUID, meiliDir)
	if err != nil {
		return fmt.Errorf("meilisearch: wait for dump: %w", err)
	}
	fmt.Fprintf(w, "meilisearch: dump complete: %s\n", dumpFile)

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

	// ---------- Step 5: run with --import-dump, wait for completion ----------
	fmt.Fprintf(w, "meilisearch: importing dump from %s...\n", dumpFile)
	importCmd := exec.CommandContext(ctx, binPath,
		"--config-file-path", filepath.Join(meiliDir, "config.toml"),
		"--import-dump", dumpFile,
	)
	importCmd.Dir = meiliDir
	importCmd.Stdout = w
	importCmd.Stderr = w
	// The import-dump flag makes Meilisearch import and exit, so we just wait.
	if err := importCmd.Run(); err != nil {
		// Meilisearch may not exit cleanly after import (it starts listening).
		// If it has been running for a while and was killed by context, that's fine.
		fmt.Fprintf(w, "meilisearch: import-dump process ended: %v\n", err)
	}

	fmt.Fprintf(w, "meilisearch: binary replaced with %s, dump imported\n", latest)
	return nil
}

// meilisearchTriggerDump calls POST /dumps and returns the task UID.
func meilisearchTriggerDump(ctx context.Context, masterKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:7700/dumps", nil)
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
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("POST /dumps returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		TaskUID int `json:"taskUid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode dump response: %w", err)
	}
	return fmt.Sprintf("%d", result.TaskUID), nil
}

// meilisearchWaitForDump polls GET /tasks/{uid} until the dump task succeeds,
// then returns the absolute path to the created dump file.
func meilisearchWaitForDump(ctx context.Context, w io.Writer, masterKey, taskUID, meiliDir string) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:7700/tasks/%s", taskUID)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(10 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case t := <-ticker.C:
			if t.After(deadline) {
				return "", fmt.Errorf("dump task %s timed out", taskUID)
			}
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return "", err
			}
			if masterKey != "" {
				req.Header.Set("Authorization", "Bearer "+masterKey)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Fprintf(w, "meilisearch: dump poll error: %v\n", err)
				continue
			}
			var task struct {
				Status  string `json:"status"`
				Details struct {
					DumpUID string `json:"dumpUid"`
				} `json:"details"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&task)
			resp.Body.Close()

			switch task.Status {
			case "succeeded":
				dumpPath := filepath.Join(meiliDir, "dumps", task.Details.DumpUID+".dump")
				return dumpPath, nil
			case "failed":
				return "", fmt.Errorf("dump task %s failed", taskUID)
			default:
				fmt.Fprintf(w, "meilisearch: dump status: %s\n", task.Status)
			}
		}
	}
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
		// Skip blank lines and pure comment lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Match both active and commented-out keys: `key = ...` or `# key = ...`
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
