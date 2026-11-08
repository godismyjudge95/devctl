package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testBin = "/opt/devctl/bin/devctl"

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return m
}

func TestMCPInstall_PrintWritesNothing(t *testing.T) {
	home := t.TempDir()
	results, err := mcpInstall(home, testBin, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range results {
		ids = append(ids, r.Client)
		if r.Written {
			t.Errorf("%s: written without --write", r.Client)
		}
	}
	if len(ids) != 5 {
		t.Errorf("clients = %v, want claude, cursor, opencode, zed, vscode", ids)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("print mode created %v", entries)
	}

	opencode := results[2].Config["mcp"].(map[string]any)["devctl"].(map[string]any)
	if cmd := opencode["command"].([]string); len(cmd) != 2 || cmd[0] != testBin || cmd[1] != "mcp" {
		t.Errorf("opencode command = %v", cmd)
	}
}

func TestMCPInstall_WriteMergesAndKeepsMode(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"}},"big":12345678901234567890}`), 0o600); err != nil {
		t.Fatal(err)
	}

	results, err := mcpInstall(home, testBin, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Client != "cursor" || !results[0].Written {
		t.Fatalf("results = %+v, want only cursor written (the only client set up)", results)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Errorf("created ~/.claude.json for a client that is not set up")
	}

	doc := readJSONFile(t, path)
	servers := doc["mcpServers"].(map[string]any)
	if servers["other"] == nil {
		t.Errorf("lost the other server: %v", servers)
	}
	devctl := servers["devctl"].(map[string]any)
	if devctl["command"] != testBin || devctl["args"].([]any)[0] != "mcp" {
		t.Errorf("devctl entry = %v", devctl)
	}
	data, _ := os.ReadFile(path)
	if !json.Valid(data) || !strings.Contains(string(data), "12345678901234567890") {
		t.Errorf("large number changed:\n%s", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 kept", info.Mode().Perm())
	}

	if _, err := mcpInstall(home, testBin, []string{"cursor"}, true); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(data) {
		t.Errorf("second write changed the file:\n%s\n---\n%s", data, again)
	}
}

func TestMCPInstall_RefusesJSONCAndUnknownClient(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "zed", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const jsonc = "// my settings\n{\"theme\": \"x\"}\n"
	if err := os.WriteFile(path, []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := mcpInstall(home, testBin, []string{"zed"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Written || results[0].Error == "" {
		t.Errorf("zed result = %+v, want an error and no write", results[0])
	}
	if data, _ := os.ReadFile(path); string(data) != jsonc {
		t.Errorf("settings.json changed:\n%s", data)
	}

	if _, err := mcpInstall(home, testBin, []string{"emacs"}, false); err == nil {
		t.Error("unknown client: want an error")
	}
}

func TestMCPInstall_ExplicitClientCreatesConfig(t *testing.T) {
	home := t.TempDir()
	results, err := mcpInstall(home, testBin, []string{"claude"}, true)
	if err != nil || !results[0].Written {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
	devctl := readJSONFile(t, filepath.Join(home, ".claude.json"))["mcpServers"].(map[string]any)["devctl"].(map[string]any)
	if devctl["type"] != "stdio" || devctl["command"] != testBin {
		t.Errorf("claude entry = %v", devctl)
	}
}
