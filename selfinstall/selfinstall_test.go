package selfinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteShellPathConfig_AppendsToExistingFiles verifies that
// writeShellPathConfig appends the devctl PATH block to each shell config file
// that already exists.
func TestWriteShellPathConfig_AppendsToExistingFiles(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"
	composerBinDir := "/home/alice/.config/composer/vendor/bin"

	// Create .bashrc and .zshrc but not .bash_profile.
	bashrc := filepath.Join(dir, ".bashrc")
	zshrc := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(bashrc, []byte("# existing bashrc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zshrc, []byte("# existing zshrc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeShellPathConfig(dir, binDir, composerBinDir); err != nil {
		t.Fatalf("writeShellPathConfig: %v", err)
	}

	// .bashrc and .zshrc should now contain the PATH block.
	for _, f := range []string{bashrc, zshrc} {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(content)
		if !strings.Contains(s, shellPathMarker) {
			t.Errorf("%s: missing PATH marker", f)
		}
		if !strings.Contains(s, binDir) {
			t.Errorf("%s: missing binDir %q", f, binDir)
		}
		if !strings.Contains(s, composerBinDir) {
			t.Errorf("%s: missing composerBinDir %q", f, composerBinDir)
		}
	}

	// .bash_profile does not exist so it should not have been created.
	if _, err := os.Stat(filepath.Join(dir, ".bash_profile")); !os.IsNotExist(err) {
		t.Error(".bash_profile should not have been created")
	}
}

// TestWriteShellPathConfig_Idempotent verifies that calling writeShellPathConfig
// twice does not duplicate the PATH block.
func TestWriteShellPathConfig_Idempotent(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"
	composerBinDir := "/home/alice/.config/composer/vendor/bin"

	bashrc := filepath.Join(dir, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := writeShellPathConfig(dir, binDir, composerBinDir); err != nil {
			t.Fatalf("call %d: writeShellPathConfig: %v", i+1, err)
		}
	}

	content, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(content), shellPathMarker)
	if count != 1 {
		t.Errorf("expected marker to appear once, got %d times\n%s", count, string(content))
	}
}

// TestRemoveShellPathConfig_RemovesBlock verifies that removeShellPathConfig
// strips the devctl PATH block added by writeShellPathConfig.
func TestRemoveShellPathConfig_RemovesBlock(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"
	composerBinDir := "/home/alice/.config/composer/vendor/bin"

	bashrc := filepath.Join(dir, ".bashrc")
	original := "# line1\n# line2\n"
	if err := os.WriteFile(bashrc, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	// Add the block.
	if err := writeShellPathConfig(dir, binDir, composerBinDir); err != nil {
		t.Fatalf("writeShellPathConfig: %v", err)
	}

	// Confirm it was added.
	after, _ := os.ReadFile(bashrc)
	if !strings.Contains(string(after), shellPathMarker) {
		t.Fatal("marker not found after write")
	}

	// Remove it.
	removeShellPathConfig(dir)

	final, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(final), shellPathMarker) {
		t.Errorf("marker still present after remove:\n%s", string(final))
	}
	if strings.Contains(string(final), binDir) {
		t.Errorf("binDir still present after remove:\n%s", string(final))
	}
	if strings.Contains(string(final), composerBinDir) {
		t.Errorf("composerBinDir still present after remove:\n%s", string(final))
	}
	// Original lines should be intact.
	if !strings.Contains(string(final), "# line1") {
		t.Errorf("original content lost:\n%s", string(final))
	}
}

// TestRemoveShellPathConfig_NoOpOnMissingMarker verifies that removeShellPathConfig
// does not modify files that never had the PATH block written.
func TestRemoveShellPathConfig_NoOpOnMissingMarker(t *testing.T) {
	dir := t.TempDir()
	bashrc := filepath.Join(dir, ".bashrc")
	original := "export FOO=bar\n"
	if err := os.WriteFile(bashrc, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	removeShellPathConfig(dir)

	content, _ := os.ReadFile(bashrc)
	if string(content) != original {
		t.Errorf("file modified unexpectedly:\ngot:  %q\nwant: %q", string(content), original)
	}
}
