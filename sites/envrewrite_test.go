package sites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteWorktreeEnvFiles_LaravelAppURL(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	src := strings.Join([]string{
		"APP_URL=https://myapp.test",
		"SESSION_DOMAIN=myapp.test",
		"SANCTUM_STATEFUL_DOMAINS=myapp.test",
		"DB_DATABASE=myapp",
		"VITE_APP_URL=http://myapp.test:5173",
	}, "\n") + "\n"
	if err := os.WriteFile(env, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	RewriteWorktreeEnvFiles(dir, "myapp.test", "myapp-feature-x.test", true)

	got := readFile(t, env)
	if !strings.Contains(got, "APP_URL=https://myapp-feature-x.test") {
		t.Errorf("APP_URL not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "SESSION_DOMAIN=myapp-feature-x.test") {
		t.Errorf("SESSION_DOMAIN not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "SANCTUM_STATEFUL_DOMAINS=myapp-feature-x.test") {
		t.Errorf("SANCTUM not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "DB_DATABASE=myapp\n") && !strings.Contains(got, "DB_DATABASE=myapp") {
		t.Errorf("DB_DATABASE should stay shared:\n%s", got)
	}
	if strings.Contains(got, "myapp.test") {
		t.Errorf("parent domain still present:\n%s", got)
	}
}

func TestRewriteWorktreeEnvFiles_LocalhostAppURL(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("APP_URL=http://localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	RewriteWorktreeEnvFiles(dir, "myapp.test", "myapp-feat.test", true)
	got := readFile(t, env)
	if !strings.Contains(got, "APP_URL=https://myapp-feat.test") {
		t.Errorf("localhost APP_URL should be replaced:\n%s", got)
	}
}

func TestRewriteWorktreeEnvFiles_WordPressConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wp-config.php")
	src := "<?php\ndefine('WP_HOME', 'http://blog.test');\ndefine('WP_SITEURL', 'http://blog.test');\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	RewriteWorktreeEnvFiles(dir, "blog.test", "blog-feature.test", true)
	got := readFile(t, path)
	if !strings.Contains(got, "https://blog-feature.test") {
		t.Errorf("WP_HOME not rewritten:\n%s", got)
	}
	if strings.Contains(got, "blog.test") {
		t.Errorf("parent domain still present:\n%s", got)
	}
}

func TestRewriteWorktreeEnvFiles_DrupalSettingsLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web", "sites", "default", "settings.local.php")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	src := "<?php\n$base_url = 'https://cms.test';\n"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	RewriteWorktreeEnvFiles(dir, "cms.test", "cms-feature.test", true)
	got := readFile(t, path)
	if !strings.Contains(got, "https://cms-feature.test") {
		t.Errorf("$base_url not rewritten:\n%s", got)
	}
}

func TestSetDotenvKey_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("FOO=bar\nAPP_URL=http://localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := setDotenvKey(path, "APP_URL", "https://x.test"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "APP_URL=https://x.test") {
		t.Errorf("got %s", got)
	}
	if !strings.Contains(got, "FOO=bar") {
		t.Errorf("lost FOO: %s", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
