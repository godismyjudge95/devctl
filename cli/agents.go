package cli

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	devctlTagOpen  = "<devctl>"
	devctlTagClose = "</devctl>"
)

// UpdateAgentsFile rewrites the <devctl> block in the canonical AGENTS.md.
// It is a no-op when the file does not exist.
func UpdateAgentsFile() error {
	path, err := defaultAgentsPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	updated := upsertDevctlBlock(string(data), generateEnvGuide())
	if updated != string(data) {
		if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
			return err
		}
	}
	return refreshAgentWrappers(path)
}

func defaultAgentsPath() (string, error) {
	if p := os.Getenv("DEVCTL_AGENTS_MD"); p != "" {
		return p, nil
	}
	home, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "AGENTS.md"), nil
}

func wrapDevctl(inner string) string {
	return devctlTagOpen + "\n" + strings.TrimSpace(inner) + "\n" + devctlTagClose
}

func upsertDevctlBlock(file, inner string) string {
	wrapped := wrapDevctl(inner)
	start := strings.Index(file, devctlTagOpen)
	end := strings.Index(file, devctlTagClose)
	if start >= 0 && end >= start {
		before := strings.TrimRight(file[:start], " \t\n")
		after := strings.TrimLeft(file[end+len(devctlTagClose):], " \t\n")
		out := before + "\n\n" + wrapped
		if after != "" {
			out += "\n\n" + after
		}
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}

	file = stripHeadingSection(file, "# Server / Environment")
	const marker = "# Browser Automation"
	if i := strings.Index(file, marker); i >= 0 {
		before := strings.TrimRight(file[:i], " \t\n")
		after := strings.TrimLeft(file[i:], " \t\n")
		out := before + "\n\n" + wrapped + "\n\n" + after
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return out
	}
	return strings.TrimRight(file, "\n") + "\n\n" + wrapped + "\n"
}

func stripHeadingSection(file, heading string) string {
	i := strings.Index(file, heading)
	if i < 0 {
		return file
	}
	rest := file[i:]
	const sep = "\n---\n"
	j := strings.Index(rest, sep)
	if j < 0 {
		return strings.TrimRight(file[:i], "\n") + "\n"
	}
	after := strings.TrimLeft(rest[j+len(sep):], "\n")
	before := strings.TrimRight(file[:i], "\n")
	if after == "" {
		return before + "\n"
	}
	return before + "\n\n" + after
}

func refreshAgentWrappers(canonical string) error {
	home, err := userHome()
	if err != nil {
		return nil
	}
	body, err := os.ReadFile(canonical)
	if err != nil {
		return err
	}
	type wrapper struct {
		path string
		head string
	}
	for _, w := range []wrapper{
		{
			path: filepath.Join(home, ".cursor", "rules", "agents.mdc"),
			head: "---\ndescription: Global agent instructions\nalwaysApply: true\n---\n\n",
		},
		{
			path: filepath.Join(home, ".vscode-server", "data", "User", "prompts", "agents.instructions.md"),
			head: "---\napplyTo: \"**\"\n---\n\n",
		},
	} {
		if _, err := os.Stat(w.path); err != nil {
			continue
		}
		if err := os.WriteFile(w.path, []byte(w.head+string(body)), 0644); err != nil {
			return err
		}
	}
	return nil
}
