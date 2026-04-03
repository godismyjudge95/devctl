package cli

import (
	"os"
	"path/filepath"
	"strings"
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
