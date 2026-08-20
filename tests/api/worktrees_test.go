//go:build integration

package apitest

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type worktreeConfig struct {
	Symlinks []string `json:"symlinks"`
	Copies   []string `json:"copies"`
}

func TestWorktreeConfig_LaravelDefaultsCopyVendor(t *testing.T) {
	dir := siteRepoDir(t, "wt-defaults")
	initGitRepoAPI(t, dir, map[string]string{
		"artisan":          "<?php\n",
		"public/index.php": "<?php\n",
		"README.md":        "app\n",
	})
	site := createTrackedSite(t, "wt-defaults.test", dir)

	body := httpGet(t, "/api/sites/"+site.ID+"/worktree-config")
	cfg := decodeJSON[worktreeConfig](t, body)
	if containsStr(cfg.Symlinks, "vendor") {
		t.Errorf("vendor must not be a default symlink: %v", cfg.Symlinks)
	}
	if !containsStr(cfg.Copies, "vendor") || !containsStr(cfg.Copies, ".env") {
		t.Errorf("expected vendor and .env in copies, got %v", cfg.Copies)
	}
}

func TestWorktreeConfig_DrupalDefaults(t *testing.T) {
	dir := siteRepoDir(t, "wt-drupal-cfg")
	initGitRepoAPI(t, dir, map[string]string{
		"composer.json":           `{"require":{"drupal/core":"^10"}}`,
		"web/index.php":           "<?php\n",
		"web/core/lib/Drupal.php": "<?php\n",
		"README.md":               "drupal\n",
	})
	site := createTrackedSite(t, "wt-drupal-cfg.test", dir)

	body := httpGet(t, "/api/sites/"+site.ID+"/worktree-config")
	cfg := decodeJSON[worktreeConfig](t, body)
	if !containsStr(cfg.Symlinks, "web/sites/default/files") {
		t.Errorf("drupal should symlink files dir, got %v", cfg.Symlinks)
	}
}

func TestWorktreeLifecycle_LaravelEnvVendorAndRemove(t *testing.T) {
	dir := siteRepoDir(t, "wt-laravel")
	initGitRepoAPI(t, dir, map[string]string{
		"artisan":             "<?php\n",
		"composer.lock":       `{"content-hash":"abc"}`,
		"public/index.php":    "<?php\n",
		"README.md":           "app\n",
		".gitignore":          "/vendor\n.env\n",
		"vendor/autoload.php": "<?php\n",
		".env":                "APP_URL=https://wt-laravel.test\nDB_DATABASE=wtlaravel\n",
	})
	runGitAPI(t, dir, "branch", "feature/cart")
	parent := createTrackedSite(t, "wt-laravel.test", dir)

	createBody, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch": "feature/cart",
	})
	if status != http.StatusCreated {
		t.Fatalf("create worktree: status %d: %s", status, createBody)
	}
	wt := decodeJSON[Site](t, createBody)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	})

	if wt.Domain != "wt-laravel-feature-cart.test" {
		t.Errorf("domain = %q", wt.Domain)
	}
	if wt.ParentSiteID == nil || *wt.ParentSiteID != parent.ID {
		t.Errorf("parent_site_id = %v", wt.ParentSiteID)
	}
	if wt.WorktreeBranch == nil || *wt.WorktreeBranch != "feature/cart" {
		t.Errorf("branch = %v", wt.WorktreeBranch)
	}

	env, err := os.ReadFile(filepath.Join(wt.RootPath, ".env"))
	if err != nil {
		t.Fatalf("read worktree .env: %v", err)
	}
	if !strings.Contains(string(env), "APP_URL=https://wt-laravel-feature-cart.test") {
		t.Errorf("APP_URL not rewritten:\n%s", env)
	}
	if !strings.Contains(string(env), "DB_DATABASE=wtlaravel") {
		t.Errorf("DB_DATABASE should stay shared:\n%s", env)
	}

	vendor := filepath.Join(wt.RootPath, "vendor")
	info, err := os.Lstat(vendor)
	if err != nil {
		t.Fatalf("vendor: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("vendor must be a copy, not a symlink")
	}

	listBody := httpGet(t, "/api/sites/"+parent.ID+"/worktrees")
	list := decodeJSON[[]Site](t, listBody)
	found := false
	for _, s := range list {
		if s.ID == wt.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("worktree missing from GET /worktrees")
	}

	delBody, delStatus := httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	if delStatus != http.StatusNoContent && delStatus != http.StatusOK {
		t.Fatalf("delete worktree: status %d: %s", delStatus, delBody)
	}
	if _, err := os.Stat(wt.RootPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still on disk: %v", err)
	}
}

func TestWorktreeCreate_NewBranch(t *testing.T) {
	dir := siteRepoDir(t, "wt-newbranch")
	initGitRepoAPI(t, dir, map[string]string{"README.md": "x\n"})
	parent := createTrackedSite(t, "wt-newbranch.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch":        "hotfix/now",
		"create_branch": true,
		"no_share":      true,
	})
	if status != http.StatusCreated {
		t.Fatalf("status %d: %s", status, body)
	}
	wt := decodeJSON[Site](t, body)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	})
	if wt.Domain != "wt-newbranch-hotfix-now.test" {
		t.Errorf("domain = %q", wt.Domain)
	}
}

func TestWorktreeCreate_WordPressRewritesConfig(t *testing.T) {
	dir := siteRepoDir(t, "wt-wp")
	initGitRepoAPI(t, dir, map[string]string{
		"wp-config.php":            "<?php\ndefine('WP_HOME', 'http://wt-wp.test');\n",
		"wp-content/uploads/.keep": "",
		"README.md":                "wp\n",
		".gitignore":               "wp-content/uploads\n",
	})
	runGitAPI(t, dir, "branch", "feature/plugin")
	parent := createTrackedSite(t, "wt-wp.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch": "feature/plugin",
	})
	if status != http.StatusCreated {
		t.Fatalf("status %d: %s", status, body)
	}
	wt := decodeJSON[Site](t, body)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	})

	cfg, err := os.ReadFile(filepath.Join(wt.RootPath, "wp-config.php"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "wt-wp-feature-plugin.test") {
		t.Errorf("WP_HOME not rewritten:\n%s", cfg)
	}
	uploads := filepath.Join(wt.RootPath, "wp-content", "uploads")
	info, err := os.Lstat(uploads)
	if err != nil {
		t.Fatalf("uploads: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("uploads should be a symlink")
	}
}

func TestWorktreeCreate_RejectsMissingBranch(t *testing.T) {
	dir := siteRepoDir(t, "wt-nobody")
	initGitRepoAPI(t, dir, map[string]string{"README.md": "x\n"})
	parent := createTrackedSite(t, "wt-nobody.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", status, body)
	}
}

func TestWorktreeCreate_RejectsNonGitSite(t *testing.T) {
	dir := siteRepoDir(t, "wt-nogit")
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chownTestuser(t, dir)
	parent := createTrackedSite(t, "wt-nogit.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch": "feature/x",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", status, body)
	}
}

func TestWorktreeCreate_DuplicateRejected(t *testing.T) {
	dir := siteRepoDir(t, "wt-dup")
	initGitRepoAPI(t, dir, map[string]string{"README.md": "x\n"})
	runGitAPI(t, dir, "branch", "feature/dup")
	parent := createTrackedSite(t, "wt-dup.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch":   "feature/dup",
		"no_share": true,
	})
	if status != http.StatusCreated {
		t.Fatalf("first create: %d: %s", status, body)
	}
	wt := decodeJSON[Site](t, body)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	})

	body, status = httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch":   "feature/dup",
		"no_share": true,
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate expected 422, got %d: %s", status, body)
	}
}

func TestWorktreeConfig_PutAndGetRoundTrip(t *testing.T) {
	dir := siteRepoDir(t, "wt-cfg-put")
	initGitRepoAPI(t, dir, map[string]string{"artisan": "<?php\n", "README.md": "x\n"})
	site := createTrackedSite(t, "wt-cfg-put.test", dir)

	payload := worktreeConfig{Copies: []string{".env"}, Symlinks: []string{"storage/logs"}}
	body, status := httpPut(t, "/api/sites/"+site.ID+"/worktree-config", payload)
	if status != http.StatusOK {
		t.Fatalf("put config: %d: %s", status, body)
	}
	got := httpGet(t, "/api/sites/"+site.ID+"/worktree-config")
	cfg := decodeJSON[worktreeConfig](t, got)
	if !containsStr(cfg.Copies, ".env") || !containsStr(cfg.Symlinks, "storage/logs") {
		t.Errorf("round trip = %+v", cfg)
	}
}

func TestWorktreeCreate_StatamicCopiesEnv(t *testing.T) {
	dir := siteRepoDir(t, "wt-statamic")
	initGitRepoAPI(t, dir, map[string]string{
		"please":           "#!/usr/bin/env php\n",
		"artisan":          "<?php\n",
		"public/index.php": "<?php\n",
		".gitignore":       ".env\n",
		".env":             "APP_URL=https://wt-statamic.test\nAPP_KEY=base64:abc\n",
		"README.md":        "statamic\n",
	})
	runGitAPI(t, dir, "branch", "feature/blueprint")
	parent := createTrackedSite(t, "wt-statamic.test", dir)

	body, status := httpPost(t, "/api/sites/"+parent.ID+"/worktrees", map[string]any{
		"branch": "feature/blueprint",
	})
	if status != http.StatusCreated {
		t.Fatalf("status %d: %s", status, body)
	}
	wt := decodeJSON[Site](t, body)
	t.Cleanup(func() {
		httpDelete(t, "/api/sites/"+parent.ID+"/worktrees/"+wt.ID)
	})
	env, err := os.ReadFile(filepath.Join(wt.RootPath, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "wt-statamic-feature-blueprint.test") {
		t.Errorf("APP_URL not rewritten:\n%s", env)
	}
}

// siteRepoDir returns a uniquely named git-repo directory that is a sibling of
// a throwaway outer dir. Worktree dest paths are computed as
// {basename(root)}-{branch-slug} next to the parent, so the basename must match
// the domain prefix we assert on.
func siteRepoDir(t *testing.T, name string) string {
	t.Helper()
	outer, err := os.MkdirTemp("", "devctl-wt-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(outer) })
	dir := filepath.Join(outer, name)
	if err := os.MkdirAll(dir, 0777); err != nil {
		t.Fatal(err)
	}
	chownTestuser(t, outer)
	return dir
}

func createTrackedSite(t *testing.T, domain, dir string) Site {
	t.Helper()
	chownTestuser(t, dir)
	body, status := httpPost(t, "/api/sites", map[string]any{
		"domain":    domain,
		"root_path": dir,
	})
	if status != http.StatusCreated {
		t.Fatalf("create site %s: %d: %s", domain, status, body)
	}
	site := decodeJSON[Site](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })
	return site
}

func initGitRepoAPI(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	runGitAPI(t, dir, "init", "-b", "main")
	runGitAPI(t, dir, "config", "user.email", "test@example.com")
	runGitAPI(t, dir, "config", "user.name", "Test")
	runGitAPI(t, dir, "config", "commit.gpgsign", "false")
	for rel, contents := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGitAPI(t, dir, "add", "-A")
	runGitAPI(t, dir, "commit", "-m", "init")
	chownTestuser(t, dir)
}

func runGitAPI(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "safe.directory=*"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func chownTestuser(t *testing.T, path string) {
	t.Helper()
	user := os.Getenv("DEVCTL_SITE_USER")
	if user == "" {
		user = "testuser"
	}
	cmd := exec.Command("chown", "-R", user+":"+user, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("chown %s: %v (%s)", path, err, out)
	}
	_ = os.Chmod(path, 0777)
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
