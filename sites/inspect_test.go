package sites

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFramework_LaravelStatamicWordPress(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "artisan"), "<?php\n")
	if got := DetectFramework(dir); got != "laravel" {
		t.Errorf("artisan → %q, want laravel", got)
	}

	dir = t.TempDir()
	mustWrite(t, filepath.Join(dir, "please"), "#!/usr/bin/env php\n")
	if got := DetectFramework(dir); got != "statamic" {
		t.Errorf("please → %q, want statamic", got)
	}

	dir = t.TempDir()
	mustWrite(t, filepath.Join(dir, "wp-config.php"), "<?php\n")
	if got := DetectFramework(dir); got != "wordpress" {
		t.Errorf("wp-config → %q, want wordpress", got)
	}
}

func TestDetectFramework_DrupalComposerAndTarball(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "composer.json"), `{"require":{"drupal/core":"^10"}}`)
	mustWrite(t, filepath.Join(dir, "web", "index.php"), "<?php\n")
	if got := DetectFramework(dir); got != "drupal" {
		t.Errorf("composer drupal/core → %q, want drupal", got)
	}
	if got := DetectPublicDir(dir); got != "web" {
		t.Errorf("drupal public dir = %q, want web", got)
	}

	dir = t.TempDir()
	mustWrite(t, filepath.Join(dir, "core", "lib", "Drupal.php"), "<?php\n")
	mustWrite(t, filepath.Join(dir, "index.php"), "<?php\n")
	if got := DetectFramework(dir); got != "drupal" {
		t.Errorf("tarball Drupal.php → %q, want drupal", got)
	}
	if got := DetectPublicDir(dir); got != "" {
		t.Errorf("tarball drupal public dir = %q, want empty", got)
	}
}

func TestDetectFramework_WordPressBedrock(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "web", "wp-config.php"), "<?php\n")
	mustWrite(t, filepath.Join(dir, "web", "index.php"), "<?php\n")
	if got := DetectFramework(dir); got != "wordpress" {
		t.Errorf("bedrock → %q, want wordpress", got)
	}
	if got := DetectPublicDir(dir); got != "web" {
		t.Errorf("bedrock public dir = %q, want web", got)
	}
}

func TestDetectFramework_CraftAndSymfony(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "composer.json"), `{"require":{"craftcms/cms":"^5"}}`)
	mustMkdir(t, filepath.Join(dir, "web"))
	if got := DetectFramework(dir); got != "craft" {
		t.Errorf("craft → %q", got)
	}
	if got := DetectPublicDir(dir); got != "web" {
		t.Errorf("craft public dir = %q, want web", got)
	}

	dir = t.TempDir()
	mustWrite(t, filepath.Join(dir, "composer.json"), `{"require":{"symfony/framework-bundle":"^7"}}`)
	if got := DetectFramework(dir); got != "symfony" {
		t.Errorf("symfony → %q", got)
	}
	if got := DetectPublicDir(dir); got != "public" {
		t.Errorf("symfony public dir = %q, want public", got)
	}
}

func TestDetectFramework_StatamicBeatsLaravel(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "artisan"), "<?php\n")
	mustWrite(t, filepath.Join(dir, "please"), "#!/usr/bin/env php\n")
	if got := DetectFramework(dir); got != "statamic" {
		t.Errorf("statamic+laravel markers → %q, want statamic", got)
	}
}

func TestInspectPath_GitAndFramework(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "artisan"), "<?php\n")
	mustMkdir(t, filepath.Join(dir, "public"))
	info := InspectPath(dir)
	if info.Framework != "laravel" || info.PublicDir != "public" {
		t.Errorf("inspect = %+v", info)
	}
	if info.IsGitRepo {
		t.Error("expected not a git repo")
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}
