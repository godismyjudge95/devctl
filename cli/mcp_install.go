package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// mcpClient is one agent app that can start `devctl mcp`. key is the
// top-level JSON key of its MCP server map.
type mcpClient struct {
	id    string
	label string
	path  func(home string) string
	key   string
	entry func(bin string) map[string]any
}

var mcpClients = []mcpClient{
	{"claude", "Claude Code", func(h string) string { return filepath.Join(h, ".claude.json") }, "mcpServers",
		func(bin string) map[string]any {
			return map[string]any{"type": "stdio", "command": bin, "args": []string{"mcp"}}
		}},
	{"cursor", "Cursor", func(h string) string { return filepath.Join(h, ".cursor", "mcp.json") }, "mcpServers",
		func(bin string) map[string]any { return map[string]any{"command": bin, "args": []string{"mcp"}} }},
	{"opencode", "OpenCode", func(h string) string { return filepath.Join(h, ".config", "opencode", "opencode.json") }, "mcp",
		func(bin string) map[string]any {
			return map[string]any{"type": "local", "command": []string{bin, "mcp"}, "enabled": true}
		}},
	{"zed", "Zed", func(h string) string { return filepath.Join(h, ".config", "zed", "settings.json") }, "context_servers",
		func(bin string) map[string]any { return map[string]any{"command": bin, "args": []string{"mcp"}} }},
	{"vscode", "VS Code", vscodeMCPPath, "servers",
		func(bin string) map[string]any {
			return map[string]any{"type": "stdio", "command": bin, "args": []string{"mcp"}}
		}},
}

func vscodeMCPPath(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")
	}
	if _, err := os.Stat(filepath.Join(home, ".vscode-server")); err == nil {
		return filepath.Join(home, ".vscode-server", "data", "User", "mcp.json")
	}
	return filepath.Join(home, ".config", "Code", "User", "mcp.json")
}

type mcpInstallResult struct {
	Client  string         `json:"client"`
	Label   string         `json:"-"`
	Path    string         `json:"path"`
	Config  map[string]any `json:"config"`
	Written bool           `json:"written"`
	Error   string         `json:"error,omitempty"`
}

func init() {
	Register(&Cmd{
		Name:        "mcp:install",
		MCP:         MCPHidden,
		Description: "Print the MCP config for Claude Code, Cursor, OpenCode, Zed, and VS Code. --write adds it to their config files",
		Usage:       "[--client=claude,cursor,opencode,zed,vscode] [--write]",
		Flags: []FlagDef{
			{Name: "client", Default: "all", Description: "Comma-separated clients: claude, cursor, opencode, zed, vscode"},
			{Name: "write", Default: "false", Description: "Write the config. Without --client, only clients whose config exists"},
		},
		Examples: []string{"devctl mcp:install", "devctl mcp:install --client=cursor --write", "devctl mcp:install --write --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mcp:install", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			only := fs.String("client", "", "")
			write := fs.Bool("write", false, "")
			if err := fs.Parse(args); err != nil {
				return err
			}
			home, err := userHome()
			if err != nil {
				return err
			}
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			var ids []string
			if *only != "" && *only != "all" {
				ids = strings.Split(*only, ",")
			}
			results, err := mcpInstall(home, bin, ids, *write)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(results)
			} else {
				printMCPInstall(results, bin, *write)
			}
			for _, r := range results {
				if r.Error != "" {
					return errors.New("some configs were not written")
				}
			}
			return nil
		},
	})
}

// mcpInstall builds the config of each chosen client and, with write, merges
// it into the client's file. With write and no ids it skips clients that are
// not set up on this machine.
func mcpInstall(home, bin string, ids []string, write bool) ([]mcpInstallResult, error) {
	chosen := mcpClients
	if len(ids) > 0 {
		chosen = nil
		for _, id := range ids {
			i := -1
			for j, mc := range mcpClients {
				if mc.id == strings.TrimSpace(id) {
					i = j
				}
			}
			if i < 0 {
				return nil, fmt.Errorf("unknown client %q (use claude, cursor, opencode, zed, vscode)", id)
			}
			chosen = append(chosen, mcpClients[i])
		}
	}
	var out []mcpInstallResult
	for _, mc := range chosen {
		path := mc.path(home)
		entry := mc.entry(bin)
		r := mcpInstallResult{
			Client: mc.id,
			Label:  mc.label,
			Path:   path,
			Config: map[string]any{mc.key: map[string]any{"devctl": entry}},
		}
		if write {
			if len(ids) == 0 && !mcpClientPresent(home, path) {
				continue
			}
			if err := writeMCPConfig(path, mc.key, entry); err != nil {
				r.Error = err.Error()
			} else {
				r.Written = true
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func mcpClientPresent(home, path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	dir := filepath.Dir(path)
	if dir == home {
		return false
	}
	_, err := os.Stat(dir)
	return err == nil
}

// writeMCPConfig sets key.devctl in the JSON file at path and keeps every
// other value. It refuses files with comments (JSONC) instead of losing them.
func writeMCPConfig(path, key string, entry map[string]any) error {
	doc := map[string]any{}
	mode := os.FileMode(0o644)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if info, serr := os.Stat(path); serr == nil {
			mode = info.Mode().Perm()
		}
		if len(bytes.TrimSpace(data)) > 0 {
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			if err := dec.Decode(&doc); err != nil {
				return fmt.Errorf("%s is not plain JSON (comments?). Add the config by hand", path)
			}
		}
	case !os.IsNotExist(err):
		return err
	}
	servers, ok := doc[key].(map[string]any)
	if !ok {
		if doc[key] != nil {
			return fmt.Errorf("%s: %q is not an object", path, key)
		}
		servers = map[string]any{}
	}
	servers["devctl"] = entry
	doc[key] = servers

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func printMCPInstall(results []mcpInstallResult, bin string, write bool) {
	if write && len(results) == 0 {
		fmt.Println("No MCP client config found. Name one with --client.")
		return
	}
	for _, r := range results {
		if r.Written {
			PrintOK("Added devctl to " + r.Label + " (" + r.Path + ")")
			continue
		}
		if r.Error != "" {
			PrintErr(fmt.Errorf("%s: %s", r.Label, r.Error))
		}
		b, _ := json.MarshalIndent(r.Config, "", "  ")
		fmt.Println(styleLabel.Render(r.Label) + "  " + styleDim.Render(r.Path))
		fmt.Println(string(b))
		if r.Client == "claude" {
			fmt.Println(styleDim.Render("or run: claude mcp add --scope user devctl -- " + bin + " mcp"))
		}
		fmt.Println()
	}
}
