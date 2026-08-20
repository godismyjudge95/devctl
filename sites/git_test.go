package sites

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugifyBranch(t *testing.T) {
	cases := map[string]string{
		"feature/my-thing": "feature-my-thing",
		"origin/foo":       "foo",
		"origin-foo":       "foo",
		"FOO_BAR":          "foo-bar",
		"v1.2.3":           "v1-2-3",
		"weird!@#branch":   "weird-branch",
		"  Feature/X  ":    "feature-x",
		"---":              "branch",
		"foo--bar":         "foo-bar",
		"feat/a_b.c":       "feat-a-b-c",
	}
	for in, want := range cases {
		got := SlugifyBranch(in)
		if got != want {
			t.Errorf("SlugifyBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultWorktreeConfig_CopiesVendorInsteadOfSymlink(t *testing.T) {
	for _, pt := range []ProjectType{
		ProjectTypeLaravel, ProjectTypeStatamic, ProjectTypeWordPress,
		ProjectTypeDrupal, ProjectTypeCraft, ProjectTypeSymfony, ProjectTypeGeneric,
	} {
		cfg := DefaultWorktreeConfig(pt)
		for _, s := range cfg.Symlinks {
			if s == "vendor" || s == "node_modules" {
				t.Errorf("%s: vendor/node_modules must be copied, not symlinked (got symlink %q)", pt, s)
			}
		}
		hasEnv := false
		hasVendor := false
		for _, c := range cfg.Copies {
			if c == ".env" {
				hasEnv = true
			}
			if c == "vendor" {
				hasVendor = true
			}
		}
		if pt != ProjectTypeGeneric && !hasEnv {
			// generic still includes .env via sharedCopies
		}
		if !hasVendor {
			t.Errorf("%s: expected vendor in copies, got %v", pt, cfg.Copies)
		}
		if !hasEnv {
			t.Errorf("%s: expected .env in copies, got %v", pt, cfg.Copies)
		}
	}

	wp := DefaultWorktreeConfig(ProjectTypeWordPress)
	if !contains(wp.Copies, "wp-config.php") {
		t.Errorf("wordpress copies missing wp-config.php: %v", wp.Copies)
	}
	drupal := DefaultWorktreeConfig(ProjectTypeDrupal)
	if !contains(drupal.Symlinks, "web/sites/default/files") {
		t.Errorf("drupal symlinks missing files dir: %v", drupal.Symlinks)
	}
}

func TestDetectProjectType_UsesFrameworkDetection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := DetectProjectType(dir); got != ProjectTypeLaravel {
		t.Errorf("got %q, want laravel", got)
	}
}

func TestCreateGitWorktree_CopiesEnvAndVendor(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{
		"artisan":             "<?php\n",
		"composer.lock":       `{"content-hash":"abc"}`,
		"README.md":           "hello\n",
		".gitignore":          "/vendor\n.env\n/node_modules\n",
		"vendor/autoload.php": "<?php // autoload\n",
		"node_modules/.bin/x": "#!/bin/sh\n",
		".env":                "APP_URL=https://myapp.test\nSESSION_DOMAIN=myapp.test\n",
	})
	runGitOk(t, parent, "branch", "feature/env")

	dest := filepath.Join(outer, "myapp-feature-env")

	cfg := DefaultWorktreeConfig(ProjectTypeLaravel)
	if err := CreateGitWorktree(parent, dest, "feature/env", false, cfg); err != nil {
		t.Fatalf("CreateGitWorktree: %v", err)
	}

	envPath := filepath.Join(dest, ".env")
	if !fileExists(envPath) {
		t.Fatal("expected .env to be copied")
	}
	// URL rewrite happens in Manager, not CreateGitWorktree — raw copy should match parent.
	got, _ := os.ReadFile(envPath)
	if !strings.Contains(string(got), "myapp.test") {
		t.Errorf(".env copy missing parent domain: %s", got)
	}

	vendor := filepath.Join(dest, "vendor")
	info, err := os.Lstat(vendor)
	if err != nil {
		t.Fatalf("vendor: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("vendor must not be a symlink")
	}
	if !fileExists(filepath.Join(vendor, "autoload.php")) {
		t.Fatal("vendor/autoload.php missing after copy")
	}

	nm := filepath.Join(dest, "node_modules")
	info, err = os.Lstat(nm)
	if err != nil {
		t.Fatalf("node_modules: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("node_modules must not be a symlink")
	}
}

func TestCreateGitWorktree_SkipsVendorWhenLockfileDiffers(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{
		"composer.lock":       `{"content-hash":"parent"}`,
		"README.md":           "main\n",
		".gitignore":          "/vendor\n",
		"vendor/autoload.php": "<?php // parent\n",
	})
	// Feature branch has a different lockfile.
	runGitOk(t, parent, "checkout", "-b", "feature/deps")
	if err := os.WriteFile(filepath.Join(parent, "composer.lock"), []byte(`{"content-hash":"feature"}`), 0644); err != nil {
		t.Fatal(err)
	}
	runGitOk(t, parent, "add", "composer.lock")
	runGitOk(t, parent, "commit", "-m", "change lock")
	runGitOk(t, parent, "checkout", "main")

	dest := filepath.Join(outer, "myapp-feature-deps")

	cfg := WorktreeSetupConfig{Copies: []string{"vendor", ".env"}}
	if err := CreateGitWorktree(parent, dest, "feature/deps", false, cfg); err != nil {
		t.Fatalf("CreateGitWorktree: %v", err)
	}
	if fileExists(filepath.Join(dest, "vendor")) {
		t.Fatal("vendor should not be copied when composer.lock differs")
	}
}

func TestCreateGitWorktree_CopiesEnvExampleWhenEnvMissing(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{
		"README.md":    "x\n",
		".gitignore":   ".env\n",
		".env.example": "APP_URL=http://localhost\n",
	})
	runGitOk(t, parent, "branch", "feature/example")

	dest := filepath.Join(outer, "myapp-feature-example")

	if err := CreateGitWorktree(parent, dest, "feature/example", false, WorktreeSetupConfig{Copies: []string{".env"}}); err != nil {
		t.Fatalf("CreateGitWorktree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !strings.Contains(string(got), "localhost") {
		t.Errorf(".env should be copied from .env.example, got %s", got)
	}
}

func TestCreateGitWorktree_SymlinkUploads(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{
		"wp-config.php":            "<?php\n",
		"wp-content/uploads/.keep": "",
		".gitignore":               "wp-content/uploads\n",
		"README.md":                "wp\n",
	})
	runGitOk(t, parent, "branch", "feature/wp")

	dest := filepath.Join(outer, "myapp-feature-wp")

	cfg := WorktreeSetupConfig{Symlinks: []string{"wp-content/uploads"}, Copies: []string{"wp-config.php"}}
	if err := CreateGitWorktree(parent, dest, "feature/wp", false, cfg); err != nil {
		t.Fatalf("CreateGitWorktree: %v", err)
	}
	link := filepath.Join(dest, "wp-content", "uploads")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("uploads: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("wp-content/uploads should be a symlink")
	}
}

func TestCreateGitWorktree_CreateNewBranch(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{"README.md": "x\n"})
	dest := filepath.Join(outer, "myapp-hotfix")

	if err := CreateGitWorktree(parent, dest, "hotfix/now", true, WorktreeSetupConfig{}); err != nil {
		t.Fatalf("CreateGitWorktree: %v", err)
	}
	if got := GetCurrentBranch(dest); got != "hotfix/now" {
		t.Errorf("branch = %q, want hotfix/now", got)
	}
}

func TestCreateGitWorktree_RejectsAlreadyCheckedOutBranch(t *testing.T) {
	outer := t.TempDir()
	parent := filepath.Join(outer, "myapp")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, parent, map[string]string{"README.md": "x\n"})
	dest := filepath.Join(outer, "myapp-main")

	err := CreateGitWorktree(parent, dest, "main", false, WorktreeSetupConfig{})
	if err == nil {
		t.Fatal("expected error checking out already-used branch")
	}
	if !strings.Contains(err.Error(), "already checked out") && !strings.Contains(err.Error(), "already used") {
		t.Errorf("error = %v, want already-checked-out message", err)
	}
}

func TestCopyPath_CopiesDirectory(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "c.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := copyPath(src, dst); err != nil {
		t.Fatalf("copyPath: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a", "b", "c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Errorf("got %q", got)
	}
}

func TestSafeRelPath(t *testing.T) {
	if safeRelPath("../etc/passwd") {
		t.Fatal("expected ../etc/passwd to be unsafe")
	}
	if safeRelPath("/tmp/x") {
		t.Fatal("expected absolute path to be unsafe")
	}
	if !safeRelPath("vendor") || !safeRelPath("web/sites/default/files") {
		t.Fatal("expected normal relative paths to be safe")
	}
}

func TestResolveWorktreeConfig(t *testing.T) {
	defaults := ResolveWorktreeConfig(WorktreeSetupConfig{}, "{}", t.TempDir())
	if len(defaults.Copies) == 0 {
		t.Fatal("empty config should resolve to defaults")
	}
	none := ResolveWorktreeConfig(WorktreeSetupConfig{NoShare: true}, "{}", t.TempDir())
	if len(none.Copies) != 0 || len(none.Symlinks) != 0 {
		t.Errorf("no_share should yield empty lists: %+v", none)
	}
	explicit := ResolveWorktreeConfig(WorktreeSetupConfig{Copies: []string{".env"}}, "{}", t.TempDir())
	if len(explicit.Copies) != 1 || explicit.Copies[0] != ".env" {
		t.Errorf("explicit copies should be kept: %+v", explicit)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func initGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	runGitOk(t, dir, "init", "-b", "main")
	runGitOk(t, dir, "config", "user.email", "test@example.com")
	runGitOk(t, dir, "config", "user.name", "Test")
	runGitOk(t, dir, "config", "commit.gpgsign", "false")
	for rel, contents := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGitOk(t, dir, "add", "-A")
	runGitOk(t, dir, "commit", "-m", "init")
}

func runGitOk(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
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
