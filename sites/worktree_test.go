package sites

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/db"
)

func TestManagerCreateWorktree_RewritesEnvAndCopiesVendor(t *testing.T) {
	outer := t.TempDir()
	parentDir := filepath.Join(outer, "laravelapp")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parentDir, map[string]string{
		"artisan":             "<?php\n",
		"composer.lock":       `{"content-hash":"abc"}`,
		"README.md":           "app\n",
		".gitignore":          "/vendor\n.env\n",
		"vendor/autoload.php": "<?php\n",
		".env":                "APP_URL=https://laravelapp.test\nDB_DATABASE=laravelapp\n",
		"public/index.php":    "<?php\n",
	})
	runGitOk(t, parentDir, "branch", "feature/checkout")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	parent, err := m.Create(ctx, CreateSiteInput{
		Domain:     "laravelapp.test",
		RootPath:   parentDir,
		PHPVersion: "8.4",
		PublicDir:  "public",
		HTTPS:      true,
		CORS:       true,
		Framework:  "laravel",
		IsGitRepo:  true,
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	site, err := m.CreateWorktree(ctx, parent.ID, "feature/checkout", false, WorktreeSetupConfig{})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveWorktree(ctx, site.ID) })

	if site.Domain != "laravelapp-feature-checkout.test" {
		t.Errorf("domain = %q", site.Domain)
	}
	if site.ParentSiteID == nil || *site.ParentSiteID != parent.ID {
		t.Errorf("parent_site_id = %v, want %s", site.ParentSiteID, parent.ID)
	}
	if site.WorktreeBranch == nil || *site.WorktreeBranch != "feature/checkout" {
		t.Errorf("branch = %v", site.WorktreeBranch)
	}
	if site.Cors != 1 {
		t.Error("worktree should inherit CORS")
	}
	if site.Https != 1 {
		t.Error("worktree should inherit HTTPS")
	}

	env := readFile(t, filepath.Join(site.RootPath, ".env"))
	if !strings.Contains(env, "APP_URL=https://laravelapp-feature-checkout.test") {
		t.Errorf("APP_URL not rewritten:\n%s", env)
	}
	if !strings.Contains(env, "DB_DATABASE=laravelapp") {
		t.Errorf("DB_DATABASE should remain shared:\n%s", env)
	}

	vendor := filepath.Join(site.RootPath, "vendor")
	info, err := os.Lstat(vendor)
	if err != nil {
		t.Fatalf("vendor: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("vendor must be a real copy, not a symlink")
	}

	// Remove cleans git + db.
	if err := m.RemoveWorktree(ctx, site.ID); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(site.RootPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists: %v", err)
	}
}

func TestManagerCreateWorktree_WordPressCopiesConfig(t *testing.T) {
	outer := t.TempDir()
	parentDir := filepath.Join(outer, "blog")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parentDir, map[string]string{
		"wp-config.php":            "<?php\ndefine('WP_HOME', 'http://blog.test');\ndefine('DB_NAME', 'blog');\n",
		"wp-content/uploads/.keep": "",
		"README.md":                "wp\n",
		".gitignore":               "wp-content/uploads\n",
	})
	runGitOk(t, parentDir, "branch", "feature/theme")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	parent, err := m.Create(ctx, CreateSiteInput{
		Domain:    "blog.test",
		RootPath:  parentDir,
		HTTPS:     true,
		Framework: "wordpress",
		IsGitRepo: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	site, err := m.CreateWorktree(ctx, parent.ID, "feature/theme", false, WorktreeSetupConfig{})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveWorktree(ctx, site.ID) })

	cfg := readFile(t, filepath.Join(site.RootPath, "wp-config.php"))
	if !strings.Contains(cfg, "https://blog-feature-theme.test") {
		t.Errorf("WP_HOME not rewritten:\n%s", cfg)
	}
	uploads := filepath.Join(site.RootPath, "wp-content", "uploads")
	info, err := os.Lstat(uploads)
	if err != nil {
		t.Fatalf("uploads: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("uploads should be a symlink to the parent")
	}
}

func TestManagerCreateWorktree_DrupalPublicDirInherited(t *testing.T) {
	outer := t.TempDir()
	parentDir := filepath.Join(outer, "cms")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parentDir, map[string]string{
		"composer.json":           `{"require":{"drupal/core":"^10"}}`,
		"web/index.php":           "<?php\n",
		"web/core/lib/Drupal.php": "<?php\n",
		".env":                    "APP_URL=https://cms.test\n",
		".gitignore":              ".env\n",
		"README.md":               "drupal\n",
	})
	runGitOk(t, parentDir, "branch", "feature/block")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	parent, err := m.Create(ctx, CreateSiteInput{
		Domain:    "cms.test",
		RootPath:  parentDir,
		PublicDir: "web",
		HTTPS:     true,
		Framework: "drupal",
		IsGitRepo: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	site, err := m.CreateWorktree(ctx, parent.ID, "feature/block", false, WorktreeSetupConfig{})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	t.Cleanup(func() { _ = m.RemoveWorktree(ctx, site.ID) })

	if site.PublicDir != "web" {
		t.Errorf("public_dir = %q, want web", site.PublicDir)
	}
	if site.Framework != "drupal" {
		t.Errorf("framework = %q, want drupal", site.Framework)
	}
}

func TestManagerCreateWorktree_DuplicatePathRejected(t *testing.T) {
	outer := t.TempDir()
	parentDir := filepath.Join(outer, "dup")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parentDir, map[string]string{"README.md": "x\n"})
	runGitOk(t, parentDir, "branch", "feature/a")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	parent, err := m.Create(ctx, CreateSiteInput{
		Domain: "dup.test", RootPath: parentDir, HTTPS: true, IsGitRepo: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	site, err := m.CreateWorktree(ctx, parent.ID, "feature/a", false, WorktreeSetupConfig{NoShare: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.RemoveWorktree(ctx, site.ID) })

	_, err = m.CreateWorktree(ctx, parent.ID, "feature/a", false, WorktreeSetupConfig{NoShare: true})
	if err == nil {
		t.Fatal("expected duplicate domain/path error")
	}
}

func TestAutoDiscover_LinkedWorktreeSeedsEnvAndParent(t *testing.T) {
	outer := t.TempDir()
	parentDir := filepath.Join(outer, "autowt")
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parentDir, map[string]string{
		"artisan":             "<?php\n",
		"public/index.php":    "<?php\n",
		".gitignore":          ".env\n/vendor\n",
		".env":                "APP_URL=https://autowt.test\n",
		"vendor/autoload.php": "<?php\n",
		"composer.lock":       `{"content-hash":"abc"}`,
		"README.md":           "auto\n",
	})
	runGitOk(t, parentDir, "branch", "feature/auto")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	_, err = m.Create(ctx, CreateSiteInput{
		Domain: "autowt.test", RootPath: parentDir, HTTPS: true, IsGitRepo: true, Framework: "laravel", PublicDir: "public",
	})
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(outer, "autowt-feature-auto")
	if err := CreateGitWorktree(parentDir, dest, "feature/auto", false, WorktreeSetupConfig{NoShare: true}); err != nil {
		t.Fatalf("git worktree add: %v", err)
	}
	// Simulate a bare `git worktree add` — no seed yet — then AutoDiscover.
	if err := os.Remove(filepath.Join(dest, ".env")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	if err := m.AutoDiscover(ctx, dest); err != nil {
		t.Fatalf("AutoDiscover: %v", err)
	}
	t.Cleanup(func() {
		site, err := m.db.GetSiteByDomain(ctx, "autowt-feature-auto.test")
		if err == nil {
			_ = m.RemoveWorktree(ctx, site.ID)
		}
	})

	site, err := m.db.GetSiteByDomain(ctx, "autowt-feature-auto.test")
	if err != nil {
		t.Fatalf("discovered site: %v", err)
	}
	if site.ParentSiteID == nil {
		t.Fatal("auto-discovered worktree missing parent_site_id")
	}
	if site.WorktreeBranch == nil || *site.WorktreeBranch != "feature/auto" {
		t.Errorf("branch = %v", site.WorktreeBranch)
	}
	env := readFile(t, filepath.Join(dest, ".env"))
	if !strings.Contains(env, "APP_URL=https://autowt-feature-auto.test") {
		t.Errorf("auto-discover did not rewrite APP_URL:\n%s", env)
	}
	info, err := os.Lstat(filepath.Join(dest, "vendor"))
	if err != nil {
		t.Fatalf("vendor: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("auto-discover vendor must be a copy")
	}
}

func TestManagerCreateWorktree_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())
	ctx := context.Background()

	parent, err := m.Create(ctx, CreateSiteInput{
		Domain: "nogit.test", RootPath: dir, HTTPS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CreateWorktree(ctx, parent.ID, "feature/x", true, WorktreeSetupConfig{})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %v, want not a git repository", err)
	}
}
