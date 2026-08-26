package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func fakeTool(name string, def bool) Tool {
	return Tool{
		Name:        name,
		Label:       name,
		Description: "test " + name,
		Default:     def,
		LatestRelease: func(_ context.Context) (Release, error) {
			return Release{Version: "1.2.3", DownloadURL: "http://example.com/" + name}, nil
		},
		DownloadTo: func(_ context.Context, _ Release, destPath string) error {
			return os.WriteFile(destPath, []byte(name+"-bin"), 0755)
		},
		InstalledVersion: func(_ context.Context, binPath string) string {
			if _, err := os.Stat(binPath); err != nil {
				return ""
			}
			return "1.0.0"
		},
	}
}

func TestStatesFor_MarksMissingAsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	got := statesFor(context.Background(), []Tool{fakeTool("mago", false)}, dir, nil)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Installed {
		t.Fatal("expected not installed")
	}
	if got[0].ID != "mago" {
		t.Fatalf("id = %q", got[0].ID)
	}
}

func TestStatesFor_MarksPresentAsInstalled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mago"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	got := statesFor(context.Background(), []Tool{fakeTool("mago", false)}, dir, nil)
	if !got[0].Installed {
		t.Fatal("expected installed")
	}
	if got[0].Version != "1.0.0" {
		t.Fatalf("version = %q", got[0].Version)
	}
}

func TestStatesFor_UpdateAvailableNormalizesV(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mago"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	got := statesFor(context.Background(), []Tool{fakeTool("mago", false)}, dir, map[string]string{
		"mago": "v1.0.0",
	})
	if got[0].UpdateAvailable {
		t.Fatal("v prefix should not flag an update")
	}

	got = statesFor(context.Background(), []Tool{fakeTool("mago", false)}, dir, map[string]string{
		"mago": "2.0.0",
	})
	if !got[0].UpdateAvailable {
		t.Fatal("expected update available")
	}
}

func TestUninstall_RemovesBinaryAndAliases(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fnm")
	alias := filepath.Join(dir, "nvm")
	if err := os.WriteFile(bin, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, alias); err != nil {
		t.Fatal(err)
	}
	tl := fakeTool("fnm", false)
	tl.Aliases = []string{"nvm"}
	if err := Uninstall(tl, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("binary still present")
	}
	if _, err := os.Stat(alias); !os.IsNotExist(err) {
		t.Fatal("alias still present")
	}
}

func TestUninstall_RejectsDefault(t *testing.T) {
	err := Uninstall(fakeTool("sqlite3", true), t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAll_InstallsDefaultOnly(t *testing.T) {
	dir := t.TempDir()
	core := fakeTool("sqlite3", true)
	opt := fakeTool("mago", false)
	var downloads []string
	core.DownloadTo = func(_ context.Context, _ Release, destPath string) error {
		downloads = append(downloads, "sqlite3")
		return os.WriteFile(destPath, []byte("x"), 0755)
	}
	opt.DownloadTo = func(_ context.Context, _ Release, destPath string) error {
		downloads = append(downloads, "mago")
		return os.WriteFile(destPath, []byte("x"), 0755)
	}
	core.InstalledVersion = func(_ context.Context, _ string) string { return "" }
	opt.InstalledVersion = func(_ context.Context, _ string) string { return "" }

	ensureAll(context.Background(), []Tool{core, opt}, dir, io.Discard)

	if len(downloads) != 1 || downloads[0] != "sqlite3" {
		t.Fatalf("downloads = %v, want [sqlite3]", downloads)
	}
	if _, err := os.Stat(filepath.Join(dir, "sqlite3")); err != nil {
		t.Fatalf("sqlite3 missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mago")); !os.IsNotExist(err) {
		t.Fatal("mago should not have been installed")
	}
}

func TestEnsureAll_UpdatesAlreadyInstalledOptIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mago"), []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	opt := fakeTool("mago", false)
	opt.InstalledVersion = func(_ context.Context, _ string) string { return "1.0.0" }
	downloaded := false
	opt.DownloadTo = func(_ context.Context, _ Release, destPath string) error {
		downloaded = true
		return os.WriteFile(destPath, []byte("new"), 0755)
	}

	ensureAll(context.Background(), []Tool{opt}, dir, io.Discard)
	if !downloaded {
		t.Fatal("expected update of already-installed opt-in tool")
	}
}

func TestEnsureAll_ContinuesOnError(t *testing.T) {
	dir := t.TempDir()
	bad := fakeTool("sqlite3", true)
	bad.LatestRelease = func(_ context.Context) (Release, error) {
		return Release{}, errors.New("network")
	}
	ok := fakeTool("also-default", true)
	ok.DownloadTo = func(_ context.Context, _ Release, destPath string) error {
		return os.WriteFile(destPath, []byte("x"), 0755)
	}
	ok.InstalledVersion = func(_ context.Context, _ string) string { return "" }

	ensureAll(context.Background(), []Tool{bad, ok}, dir, io.Discard)
	if _, err := os.Stat(filepath.Join(dir, "also-default")); err != nil {
		t.Fatalf("second default tool should still install: %v", err)
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("sqlite3"); !ok {
		t.Fatal("sqlite3 should be registered")
	}
	got, ok := Lookup("wp")
	if !ok {
		t.Fatal("wp should be registered")
	}
	if got.Label != "WP-CLI" {
		t.Fatalf("wp label = %q, want WP-CLI", got.Label)
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("unknown tool should miss")
	}
}
