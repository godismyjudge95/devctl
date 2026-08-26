package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertDevctlBlock_ReplacesExistingTags(t *testing.T) {
	in := "# Keep me\n\n<devctl>\nold\n</devctl>\n\n# After\n"
	out := upsertDevctlBlock(in, "new inner")
	if !strings.Contains(out, "<devctl>\nnew inner\n</devctl>") {
		t.Fatalf("upsert did not replace inner block:\n%s", out)
	}
	if !strings.Contains(out, "# Keep me") || !strings.Contains(out, "# After") {
		t.Fatalf("upsert dropped surrounding text:\n%s", out)
	}
	if strings.Contains(out, "old") {
		t.Fatalf("upsert left old inner content:\n%s", out)
	}
}

func TestUpsertDevctlBlock_ReplacesServerEnvironmentSection(t *testing.T) {
	in := "# Behavioral Guidelines\n\nrules\n\n---\n\n# Server / Environment\n\n**This is NOT a DDEV project.**\n\n---\n\n# Browser Automation\n\nbrowser\n"
	out := upsertDevctlBlock(in, "guide here")
	if strings.Contains(out, "# Server / Environment") {
		t.Fatalf("old heading still present:\n%s", out)
	}
	if strings.Contains(out, "This is NOT a DDEV project") {
		t.Fatalf("old environment prose still present:\n%s", out)
	}
	if !strings.Contains(out, "<devctl>\nguide here\n</devctl>") {
		t.Fatalf("missing wrapped block:\n%s", out)
	}
	if !strings.Contains(out, "# Behavioral Guidelines") || !strings.Contains(out, "# Browser Automation") {
		t.Fatalf("lost sibling sections:\n%s", out)
	}
}

func TestUpsertDevctlBlock_AppendsWhenNoAnchor(t *testing.T) {
	in := "# Only this\n"
	out := upsertDevctlBlock(in, "guide")
	if !strings.HasSuffix(strings.TrimSpace(out), "</devctl>") {
		t.Fatalf("expected block at end:\n%s", out)
	}
	if !strings.Contains(out, "# Only this") {
		t.Fatalf("lost original content:\n%s", out)
	}
}

func TestUpdateAgentsFile_NoopsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")
	t.Setenv("DEVCTL_AGENTS_MD", "")

	if err := UpdateAgentsFile(); err != nil {
		t.Fatalf("UpdateAgentsFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "AGENTS.md")); err == nil {
		t.Fatal("UpdateAgentsFile created AGENTS.md — expected no-op")
	}
}

func TestUpdateAgentsFile_RewritesTaggedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := "# Intro\n\n<devctl>\nstale\n</devctl>\n\n# Rest\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVCTL_AGENTS_MD", path)
	t.Setenv("HOME", dir)
	t.Setenv("SUDO_USER", "")

	if err := UpdateAgentsFile(); err != nil {
		t.Fatalf("UpdateAgentsFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "stale") {
		t.Fatalf("stale block remains:\n%s", got)
	}
	if !strings.Contains(got, "<devctl>") || !strings.Contains(got, "</devctl>") {
		t.Fatalf("missing tags:\n%s", got)
	}
	if !strings.Contains(got, "services:credentials") {
		t.Fatalf("guide missing credentials command:\n%s", got)
	}
	if !strings.Contains(got, "# Intro") || !strings.Contains(got, "# Rest") {
		t.Fatalf("lost surrounding sections:\n%s", got)
	}
}
