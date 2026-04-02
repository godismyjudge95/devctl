package selfinstall

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// updateShellFile
// ---------------------------------------------------------------------------

// TestUpdateShellFile_AppendsToExistingFile verifies that the PATH block is
// appended when the file exists but has no existing sentinel.
func TestUpdateShellFile_AppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"
	f := filepath.Join(dir, ".zshenv")
	if err := os.WriteFile(f, []byte("# existing zshenv\n"), 0644); err != nil {
		t.Fatal(err)
	}

	block := "\n" + pathBlock(binDir)
	if err := updateShellFile(f, block, true, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("updateShellFile: %v", err)
	}

	content, _ := os.ReadFile(f)
	s := string(content)
	if !strings.Contains(s, pathBlockStart) {
		t.Error("missing start sentinel")
	}
	if !strings.Contains(s, pathBlockEnd) {
		t.Error("missing end sentinel")
	}
	if !strings.Contains(s, binDir) {
		t.Errorf("missing binDir %q", binDir)
	}
	// Original content must be preserved.
	if !strings.Contains(s, "# existing zshenv") {
		t.Error("original content lost")
	}
}

// TestUpdateShellFile_CreatesFileWhenMustCreate verifies that a missing file is
// created when mustCreate is true.
func TestUpdateShellFile_CreatesFileWhenMustCreate(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".zshenv")
	block := "\n" + pathBlock("/some/bin")

	if err := updateShellFile(f, block, true, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("updateShellFile: %v", err)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	content, _ := os.ReadFile(f)
	if !strings.Contains(string(content), pathBlockStart) {
		t.Error("block not written to new file")
	}
}

// TestUpdateShellFile_NoOpWhenMissingAndNotMustCreate verifies that a missing
// file is NOT created when mustCreate is false.
func TestUpdateShellFile_NoOpWhenMissingAndNotMustCreate(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".bash_profile")
	block := "\n" + pathBlock("/some/bin")

	if err := updateShellFile(f, block, false, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Error("file should not have been created")
	}
}

// TestUpdateShellFile_IdempotentOnSameBinDir verifies that calling
// updateShellFile twice with the same binDir does not duplicate the block.
func TestUpdateShellFile_IdempotentOnSameBinDir(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"
	f := filepath.Join(dir, ".bashrc")
	if err := os.WriteFile(f, []byte("# existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	block := "\n" + pathBlock(binDir)
	for i := 0; i < 2; i++ {
		if err := updateShellFile(f, block, true, os.Getuid(), os.Getgid()); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	content, _ := os.ReadFile(f)
	s := string(content)
	if got := strings.Count(s, pathBlockStart); got != 1 {
		t.Errorf("expected start sentinel once, got %d\n%s", got, s)
	}
}

// TestUpdateShellFile_ReplacesBlockWhenBinDirChanges verifies that re-running
// with a different binDir replaces the old block in-place (no duplication).
func TestUpdateShellFile_ReplacesBlockWhenBinDirChanges(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".zshenv")
	if err := os.WriteFile(f, []byte("# top\n"), 0644); err != nil {
		t.Fatal(err)
	}

	oldBin := "/old/bin"
	newBin := "/new/bin"

	if err := updateShellFile(f, "\n"+pathBlock(oldBin), true, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if err := updateShellFile(f, "\n"+pathBlock(newBin), true, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(f)
	s := string(content)

	if strings.Contains(s, oldBin) {
		t.Errorf("old binDir still present:\n%s", s)
	}
	if !strings.Contains(s, newBin) {
		t.Errorf("new binDir not present:\n%s", s)
	}
	if got := strings.Count(s, pathBlockStart); got != 1 {
		t.Errorf("expected start sentinel once, got %d\n%s", got, s)
	}
	// Original content above the block must survive.
	if !strings.Contains(s, "# top") {
		t.Errorf("content above block was lost:\n%s", s)
	}
}

// ---------------------------------------------------------------------------
// removeBlockFromFile
// ---------------------------------------------------------------------------

// TestRemoveBlockFromFile_RemovesBlock verifies that the sentinel block is
// cleanly removed and the surrounding content is preserved.
func TestRemoveBlockFromFile_RemovesBlock(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".bashrc")
	original := "# line1\n# line2\n"
	if err := os.WriteFile(f, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	binDir := "/home/alice/sites/server/bin"
	if err := updateShellFile(f, "\n"+pathBlock(binDir), true, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("updateShellFile: %v", err)
	}

	removeBlockFromFile(f)

	final, _ := os.ReadFile(f)
	s := string(final)
	if strings.Contains(s, pathBlockStart) {
		t.Errorf("start sentinel still present:\n%s", s)
	}
	if strings.Contains(s, binDir) {
		t.Errorf("binDir still present:\n%s", s)
	}
	if !strings.Contains(s, "# line1") {
		t.Errorf("original content lost:\n%s", s)
	}
}

// TestRemoveBlockFromFile_NoOpWhenNoSentinel verifies that files without the
// sentinel block are not modified.
func TestRemoveBlockFromFile_NoOpWhenNoSentinel(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".bashrc")
	original := "export FOO=bar\n"
	if err := os.WriteFile(f, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	removeBlockFromFile(f)

	content, _ := os.ReadFile(f)
	if string(content) != original {
		t.Errorf("file modified unexpectedly:\ngot:  %q\nwant: %q", string(content), original)
	}
}

// TestRemoveBlockFromFile_NoOpOnMissingFile verifies that a missing file does
// not cause an error.
func TestRemoveBlockFromFile_NoOpOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	removeBlockFromFile(filepath.Join(dir, "does-not-exist"))
	// No panic or error expected.
}

// ---------------------------------------------------------------------------
// shellFromPasswdLine
// ---------------------------------------------------------------------------

func TestShellFromPasswdLine(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"alice:x:1000:1000::/home/alice:/usr/bin/zsh", "zsh"},
		{"bob:x:1001:1001::/home/bob:/bin/bash", "bash"},
		{"root:x:0:0:root:/root:/bin/sh", "sh"},
		{"bad-line", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := shellFromPasswdLine(c.line)
		if got != c.want {
			t.Errorf("shellFromPasswdLine(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// shellTargets
// ---------------------------------------------------------------------------

func TestShellTargets_Zsh(t *testing.T) {
	dir := t.TempDir()
	targets := shellTargets("zsh", dir)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d: %v", len(targets), targets)
	}
	if !strings.HasSuffix(targets[0].path, ".zshenv") {
		t.Errorf("expected .zshenv, got %q", targets[0].path)
	}
	if !targets[0].mustCreate {
		t.Error("zshenv target should have mustCreate=true")
	}
}

func TestShellTargets_Bash_NoExtraFiles(t *testing.T) {
	dir := t.TempDir()
	// Only .bashrc should be mandatory; .bash_profile/.profile are optional.
	targets := shellTargets("bash", dir)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target (no optional files exist), got %d", len(targets))
	}
	if !strings.HasSuffix(targets[0].path, ".bashrc") {
		t.Errorf("expected .bashrc, got %q", targets[0].path)
	}
	if !targets[0].mustCreate {
		t.Error(".bashrc should have mustCreate=true")
	}
}

func TestShellTargets_Bash_WithBashProfile(t *testing.T) {
	dir := t.TempDir()
	// Create .bash_profile so it should be included.
	if err := os.WriteFile(filepath.Join(dir, ".bash_profile"), []byte("# bp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	targets := shellTargets("bash", dir)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
}

func TestShellTargets_UnknownShell_NoProfileFile(t *testing.T) {
	dir := t.TempDir()
	// No .profile exists — should return nothing.
	targets := shellTargets("fish", dir)
	if len(targets) != 0 {
		t.Errorf("expected 0 targets for unknown shell without .profile, got %d", len(targets))
	}
}

func TestShellTargets_UnknownShell_WithProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".profile"), []byte("# p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	targets := shellTargets("fish", dir)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if !strings.HasSuffix(targets[0].path, ".profile") {
		t.Errorf("expected .profile, got %q", targets[0].path)
	}
}

// ---------------------------------------------------------------------------
// WritePATHSetup — file ownership
// ---------------------------------------------------------------------------

// TestWritePATHSetup_FileOwnedByTargetUser verifies that shell config files
// written by WritePATHSetup are owned by the target user (uid/gid), not root.
// This matters because devctl install runs as root but should not leave files
// in the user's home directory owned by root.
func TestWritePATHSetup_FileOwnedByTargetUser(t *testing.T) {
	dir := t.TempDir()
	binDir := "/home/alice/sites/server/bin"

	uid := os.Getuid()
	gid := os.Getgid()

	// Determine the current user's username so getUserShell can find their
	// login shell. Using a non-existent username (like a shell name) would
	// cause getUserShell to return "" and write nothing.
	cu, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	username := cu.Username

	if err := WritePATHSetup(binDir, dir, username, uid, gid); err != nil {
		t.Fatalf("WritePATHSetup: %v", err)
	}

	// Find whichever config file was written (depends on the user's shell).
	shell := getUserShell(username)
	targets := shellTargets(shell, dir)
	if len(targets) == 0 {
		t.Skipf("no shell config targets for shell %q — cannot verify ownership", shell)
	}
	// Check the first (primary) target.
	writtenFile := targets[0].path
	if !targets[0].mustCreate {
		// Primary target is optional and won't be created. Check others.
		var found string
		for _, tgt := range targets {
			if tgt.mustCreate {
				found = tgt.path
				break
			}
		}
		if found == "" {
			t.Skipf("shell %q has no mustCreate target — cannot verify ownership", shell)
		}
		writtenFile = found
	}

	info, statErr := os.Stat(writtenFile)
	if statErr != nil {
		t.Fatalf("stat %s: %v", writtenFile, statErr)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("cannot read syscall.Stat_t on this platform")
	}
	if int(stat.Uid) != uid {
		t.Errorf("uid: got %d, want %d", stat.Uid, uid)
	}
	if int(stat.Gid) != gid {
		t.Errorf("gid: got %d, want %d", stat.Gid, gid)
	}
}

// ---------------------------------------------------------------------------
// detectServerRoot — reads from existing service file on reinstall
// ---------------------------------------------------------------------------

// TestDetectServerRoot_ReadsFromServiceFile verifies that detectServerRoot
// correctly parses the DEVCTL_SERVER_ROOT value from a systemd service file.
// This is the path used on reinstall to avoid defaulting to ~/sites when the
// real root is somewhere else (e.g. ~/ddev/sites/server).
func TestDetectServerRoot_ReadsFromServiceFile(t *testing.T) {
	dir := t.TempDir()
	serviceFile := filepath.Join(dir, "devctl.service")

	content := `[Unit]
Description=devctl

[Service]
ExecStart=/home/daniel/ddev/sites/server/devctl/devctl
Environment=HOME=/home/daniel
Environment=DEVCTL_SITE_USER=daniel
Environment=DEVCTL_SERVER_ROOT=/home/daniel/ddev/sites/server

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := detectServerRoot(serviceFile)
	want := "/home/daniel/ddev/sites/server"
	if got != want {
		t.Errorf("detectServerRoot = %q, want %q", got, want)
	}
}

// TestDetectServerRoot_ReturnsEmptyWhenMissing verifies that detectServerRoot
// returns an empty string when the service file has no DEVCTL_SERVER_ROOT line.
func TestDetectServerRoot_ReturnsEmptyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	serviceFile := filepath.Join(dir, "devctl.service")

	content := `[Service]
ExecStart=/usr/local/bin/devctl
`
	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := detectServerRoot(serviceFile)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// resolveSitesDir — uses existing service file default on reinstall
// ---------------------------------------------------------------------------

// TestResolveSitesDir_UsesDetectedServerRootOnReinstall verifies that when no
// --sites-dir flag is given and an existing service file contains
// DEVCTL_SERVER_ROOT, resolveSitesDir uses the parent of that value as the
// default (rather than ~/sites). This prevents the reinstall bug where
// WritePATHSetup would write the wrong bin directory to ~/.zshenv.
func TestResolveSitesDir_UsesDetectedServerRootOnReinstall(t *testing.T) {
	tmpDir := t.TempDir()
	serviceFile := filepath.Join(tmpDir, "devctl.service")

	content := `[Service]
Environment=DEVCTL_SERVER_ROOT=/home/daniel/ddev/sites/server
`
	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// flagVal="" means no --sites-dir flag; skipPrompt=true avoids stdin.
	got, err := resolveSitesDir("", "/home/daniel", true, nil, serviceFile)
	if err != nil {
		t.Fatalf("resolveSitesDir: %v", err)
	}
	want := "/home/daniel/ddev/sites"
	if got != want {
		t.Errorf("resolveSitesDir = %q, want %q", got, want)
	}
}

// TestResolveSitesDir_FallsBackToDefaultWhenNoServiceFile verifies that when
// there is no existing service file, resolveSitesDir falls back to ~/sites.
func TestResolveSitesDir_FallsBackToDefaultWhenNoServiceFile(t *testing.T) {
	tmpDir := t.TempDir()
	missingFile := filepath.Join(tmpDir, "devctl.service") // does not exist

	got, err := resolveSitesDir("", "/home/alice", true, nil, missingFile)
	if err != nil {
		t.Fatalf("resolveSitesDir: %v", err)
	}
	want := "/home/alice/sites"
	if got != want {
		t.Errorf("resolveSitesDir = %q, want %q", got, want)
	}
}
