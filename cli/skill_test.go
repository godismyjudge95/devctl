package cli

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestWriteSkill_CreatesFile verifies that WriteSkill writes a file at the
// given path.
func TestWriteSkill_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: unexpected error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("WriteSkill: file not created at %s: %v", path, err)
	}
}

// TestWriteSkill_CreatesParentDirs verifies that WriteSkill creates any
// missing parent directories.
func TestWriteSkill_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deeply", "nested", "dir", "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: unexpected error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("WriteSkill: file not created at %s: %v", path, err)
	}
}

// TestWriteSkill_ContentHasFrontmatter verifies that the generated skill file
// starts with valid YAML frontmatter containing the required OpenCode fields.
func TestWriteSkill_ContentHasFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		t.Error("WriteSkill: content does not start with YAML frontmatter delimiter '---'")
	}
	for _, want := range []string{"name: devctl-cli", "description:", "compatibility: opencode"} {
		if !strings.Contains(content, want) {
			t.Errorf("WriteSkill: frontmatter missing field %q", want)
		}
	}
	if strings.Contains(content, "# Skill: devctl-cli") {
		t.Error("WriteSkill: content should not contain plain heading '# Skill: devctl-cli'")
	}
}

// TestWriteSkill_ContentContainsAllNamespaces verifies that the generated
// skill file contains sections for all command namespaces.
func TestWriteSkill_ContentContainsAllNamespaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)

	wantSections := []string{
		"## services: commands",
		"## sites: commands",
		"## php: commands",
		"## logs: commands",
		"## dumps: commands",
		"## spx: commands",
		"## mail: commands",
		"## dns: commands",
		"## tls: commands",
		"## settings: commands",
		"## devctl: commands",
	}
	for _, section := range wantSections {
		if !strings.Contains(content, section) {
			t.Errorf("WriteSkill: content missing section %q", section)
		}
	}
}

// TestWriteSkill_ContentContainsGlobalFlags verifies the skill file documents
// the global flags.
func TestWriteSkill_ContentContainsGlobalFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "--json") {
		t.Error("WriteSkill: content missing --json flag documentation")
	}
	if !strings.Contains(content, "--addr") {
		t.Error("WriteSkill: content missing --addr flag documentation")
	}
}

// TestWriteSkill_ContentContainsKeyCommands verifies a representative sample
// of commands appear in the generated skill file.
func TestWriteSkill_ContentContainsKeyCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	wantCommands := []string{
		"services:list",
		"services:restart",
		"sites:list",
		"logs:tail",
		"mail:list",
		"settings:get",
		"settings:set",
		"devctl:skill",
		"services:update",
	}
	for _, cmd := range wantCommands {
		if !strings.Contains(content, cmd) {
			t.Errorf("WriteSkill: content missing command %q", cmd)
		}
	}
}

// TestWriteSkill_IsIdempotent verifies that calling WriteSkill twice
// overwrites the file with the same content without error.
func TestWriteSkill_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("first WriteSkill: %v", err)
	}
	first, _ := os.ReadFile(path)

	if err := WriteSkill(path); err != nil {
		t.Fatalf("second WriteSkill: %v", err)
	}
	second, _ := os.ReadFile(path)

	if string(first) != string(second) {
		t.Error("WriteSkill: second call produced different content than first")
	}
}

// TestSkillInstalled_ReturnsFalseWhenAbsent verifies SkillInstalled returns
// false when the file does not exist.
func TestSkillInstalled_ReturnsFalseWhenAbsent(t *testing.T) {
	// Point HOME to a temp dir that has no skill file.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "") // ensure userHome() uses HOME, not SUDO_USER

	if SkillInstalled() {
		t.Error("SkillInstalled: expected false when skill file is absent")
	}
}

// TestSkillInstalled_ReturnsTrueWhenPresent verifies SkillInstalled returns
// true after the file has been written.
func TestSkillInstalled_ReturnsTrueWhenPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")

	skillPath := filepath.Join(dir, skillDir, skillFile)
	if err := WriteSkill(skillPath); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	if !SkillInstalled() {
		t.Error("SkillInstalled: expected true after WriteSkill")
	}
}

// TestUpdateSkillIfInstalled_NoopsWhenAbsent verifies that
// UpdateSkillIfInstalled does not create a file when none exists.
func TestUpdateSkillIfInstalled_NoopsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")

	UpdateSkillIfInstalled()

	skillPath := filepath.Join(dir, skillDir, skillFile)
	if _, err := os.Stat(skillPath); err == nil {
		t.Error("UpdateSkillIfInstalled: created file when it did not exist — expected no-op")
	}
}

// TestUpdateSkillIfInstalled_RegeneratesWhenPresent verifies that
// UpdateSkillIfInstalled overwrites an existing file.
func TestUpdateSkillIfInstalled_RegeneratesWhenPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")

	skillPath := filepath.Join(dir, skillDir, skillFile)
	if err := WriteSkill(skillPath); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	// Overwrite with known sentinel content.
	if err := os.WriteFile(skillPath, []byte("stale content"), 0644); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}

	UpdateSkillIfInstalled()

	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile after UpdateSkillIfInstalled: %v", err)
	}
	if string(data) == "stale content" {
		t.Error("UpdateSkillIfInstalled: file content was not regenerated")
	}
	if !strings.Contains(string(data), "devctl") {
		t.Error("UpdateSkillIfInstalled: regenerated content does not look like a skill file")
	}
}

// TestDefaultSkillPath_ContainsExpectedComponents verifies that the default
// skill path ends with the expected directory and filename components.
func TestDefaultSkillPath_ContainsExpectedComponents(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")

	path, err := DefaultSkillPath()
	if err != nil {
		t.Fatalf("DefaultSkillPath: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join(skillDir, skillFile)) {
		t.Errorf("DefaultSkillPath: expected path ending with %q, got %q",
			filepath.Join(skillDir, skillFile), path)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("DefaultSkillPath: expected path under HOME %q, got %q", dir, path)
	}
}

// ---------------------------------------------------------------------------
// File permissions and ownership
// ---------------------------------------------------------------------------

// TestWriteSkill_FileMode verifies that the written skill file has permission
// bits 0644 — readable by all, writable only by the owner.
func TestWriteSkill_FileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("file mode: got %04o, want 0644", got)
	}
}

// TestWriteSkill_CreatedDirMode verifies that directories created by
// WriteSkill (when they do not already exist) have permission bits 0755.
func TestWriteSkill_CreatedDirMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-parent", "SKILL.md")

	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0755 {
		t.Errorf("created directory mode: got %04o, want 0755", got)
	}
}

// TestDefaultSkillPath_UsesSudoUserHome verifies that when the process is
// running as root and SUDO_USER is set, DefaultSkillPath returns a path under
// the non-root user's home directory rather than /root.
//
// This only runs as root because the path resolution bug only surfaces during
// "sudo devctl install", where uid=0 and SUDO_USER=<real user>.
func TestDefaultSkillPath_UsesSudoUserHome(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("only meaningful when running as root (simulating sudo devctl install)")
	}

	nonRootUser := findNonRootUser(t)
	if nonRootUser == "" {
		t.Skip("no non-root user with a /home/<user> directory found")
	}

	t.Setenv("SUDO_USER", nonRootUser)

	path, err := DefaultSkillPath()
	if err != nil {
		t.Fatalf("DefaultSkillPath: %v", err)
	}

	wantPrefix := filepath.Join("/home", nonRootUser)
	if !strings.HasPrefix(path, wantPrefix) {
		t.Errorf("DefaultSkillPath = %q; want path under %q\n"+
			"(running as root with SUDO_USER=%s — skill must not be installed under /root)",
			path, wantPrefix, nonRootUser)
	}
}

// TestWriteSkill_FileOwnedBySudoUser verifies that when devctl install is run
// as root with SUDO_USER set, WriteSkill creates the skill file owned by
// SUDO_USER's uid/gid, not root (uid=0).
//
// This test documents a known bug: WriteSkill currently does NOT chown the
// file after writing it, so the skill ends up root-owned even though the
// install was invoked via "sudo devctl install" by a non-root user.
// The test is expected to FAIL until WriteSkill is fixed to chown the file
// and its parent directory to SUDO_USER's uid/gid.
func TestWriteSkill_FileOwnedBySudoUser(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("only meaningful when running as root (simulating sudo devctl install)")
	}

	nonRootUser := findNonRootUser(t)
	if nonRootUser == "" {
		t.Skip("no non-root user with a /home/<user> directory found")
	}

	u, err := user.Lookup(nonRootUser)
	if err != nil {
		t.Skipf("user.Lookup(%q): %v", nonRootUser, err)
	}
	wantUID, _ := strconv.Atoi(u.Uid)
	wantGID, _ := strconv.Atoi(u.Gid)

	t.Setenv("SUDO_USER", nonRootUser)

	// Use a newly created subdirectory so both the directory and the file
	// ownership are exercised. The subdir does not exist yet — WriteSkill must
	// create it AND chown it.
	dir := t.TempDir()
	newDir := filepath.Join(dir, "new-subdir")
	path := filepath.Join(newDir, "SKILL.md")
	if err := WriteSkill(path); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}

	for _, check := range []struct {
		label string
		target string
	}{
		{"file", path},
		{"created directory", newDir},
	} {
		info, err := os.Stat(check.target)
		if err != nil {
			t.Fatalf("stat %s (%s): %v", check.label, check.target, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Skip("cannot read syscall.Stat_t on this platform")
		}
		// These assertions will FAIL until WriteSkill is fixed to chown.
		if int(stat.Uid) != wantUID {
			t.Errorf("%s uid: got %d, want %d (user %q) — WriteSkill does not chown to SUDO_USER",
				check.label, stat.Uid, wantUID, nonRootUser)
		}
		if int(stat.Gid) != wantGID {
			t.Errorf("%s gid: got %d, want %d (user %q) — WriteSkill does not chown to SUDO_USER",
				check.label, stat.Gid, wantGID, nonRootUser)
		}
	}
}

// findNonRootUser returns the username of the first non-root user found in the
// system whose home directory exists under /home/. It prefers DEVCTL_SITE_USER
// (set automatically in the standard test container) so results are
// deterministic. Returns "" if no suitable user is found.
func findNonRootUser(t *testing.T) string {
	t.Helper()

	// Prefer the well-known test container user.
	if candidate := os.Getenv("DEVCTL_SITE_USER"); candidate != "" {
		homeBase := filepath.Join("/home", candidate)
		if _, err := os.Stat(homeBase); err == nil {
			return candidate
		}
	}

	// Fallback: walk /etc/passwd for any non-root user with a /home/ entry.
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 7)
		if len(parts) < 7 {
			continue
		}
		if parts[0] == "root" {
			continue
		}
		homeName := strings.TrimSpace(parts[5])
		if !strings.HasPrefix(homeName, "/home/") {
			continue
		}
		if _, err := os.Stat(homeName); err == nil {
			return parts[0]
		}
	}
	return ""
}
