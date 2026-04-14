package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const skillDir = ".agents/skills/devctl-cli"
const skillFile = "SKILL.md"

func init() {
	Register(&Cmd{
		Name:        "devctl:skill",
		Description: "Generate an OpenCode agent skill describing all CLI commands",
		Usage:       "[--output=path]",
		Flags: []FlagDef{
			{
				Name:        "output",
				Default:     "~/.agents/skills/devctl-cli/SKILL.md",
				Description: "Path to write the skill file",
			},
		},
		Examples: []string{
			"devctl devctl:skill",
			"devctl devctl:skill --output=/custom/path/SKILL.md",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			outputPath := ""
			for _, a := range args {
				if strings.HasPrefix(a, "--output=") {
					outputPath = strings.TrimPrefix(a, "--output=")
				}
			}
			if outputPath == "" {
				home, err := userHome()
				if err != nil {
					return fmt.Errorf("cannot determine home directory: %w", err)
				}
				outputPath = filepath.Join(home, skillDir, skillFile)
			}
			// Expand ~ manually if needed
			if strings.HasPrefix(outputPath, "~/") {
				home, err := userHome()
				if err != nil {
					return fmt.Errorf("cannot determine home directory: %w", err)
				}
				outputPath = filepath.Join(home, outputPath[2:])
			}

			if err := WriteSkill(outputPath); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "path": outputPath})
				return nil
			}
			PrintOK("Skill written to " + outputPath)
			return nil
		},
	})
}

// DefaultSkillPath returns the default path for the CLI skill file.
func DefaultSkillPath() (string, error) {
	home, err := userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, skillDir, skillFile), nil
}

// SkillInstalled reports whether the CLI skill file already exists.
func SkillInstalled() bool {
	path, err := DefaultSkillPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// WriteSkill generates the SKILL.md at the given path, creating parent dirs.
// When running as root with SUDO_USER set (i.e. under sudo), it chowns the
// newly created directories and the file to SUDO_USER's uid/gid so the skill
// is not left root-owned inside the user's home directory.
func WriteSkill(outputPath string) error {
	dir := filepath.Dir(outputPath)

	// Record the first directory that does not yet exist so we can chown
	// the entire newly created subtree after MkdirAll.
	newRoot := firstMissingDir(dir)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	content := generateSkillContent()
	if err := os.WriteFile(outputPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}

	// Fix ownership when invoked under sudo so files are owned by the real
	// user, not root.
	if err := chownToSudoUser(outputPath, newRoot); err != nil {
		return fmt.Errorf("chown skill: %w", err)
	}
	return nil
}

// firstMissingDir returns the first path component of p (deepest ancestor
// that does not yet exist). Returns "" when p already exists. Used to track
// which directories WriteSkill is about to create so they can be chowned.
func firstMissingDir(p string) string {
	if _, err := os.Stat(p); err == nil {
		return "" // already exists
	}
	ancestor := p
	for {
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			// Reached the filesystem root — everything is new.
			return p
		}
		if _, err := os.Stat(parent); err == nil {
			// parent exists, ancestor does not → ancestor is the first new dir.
			return ancestor
		}
		ancestor = parent
	}
}

// chownToSudoUser chowns path and, if newRoot is non-empty, the entire
// directory tree rooted at newRoot, to SUDO_USER's uid/gid.
// It is a no-op when not running as root or when SUDO_USER is not set.
func chownToSudoUser(path, newRoot string) error {
	if os.Getuid() != 0 {
		return nil
	}
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return nil
	}
	u, err := user.Lookup(sudoUser)
	if err != nil {
		return fmt.Errorf("lookup SUDO_USER %q: %w", sudoUser, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid for %q: %w", sudoUser, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse gid for %q: %w", sudoUser, err)
	}

	// Chown the newly created directory tree (covers all intermediate dirs).
	if newRoot != "" {
		if err := filepath.Walk(newRoot, func(p string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(p, uid, gid)
		}); err != nil {
			return fmt.Errorf("chown directory tree %q: %w", newRoot, err)
		}
	}

	// Always chown the file itself (handles the case where the dir already existed).
	return os.Chown(path, uid, gid)
}

// UpdateSkillIfInstalled silently regenerates the skill file if it exists.
// Safe to call in a goroutine on daemon startup.
func UpdateSkillIfInstalled() {
	path, err := DefaultSkillPath()
	if err != nil {
		return
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	_ = WriteSkill(path)
}

// generateSkillContent builds the SKILL.md content from the command registry.
// The output describes capabilities by namespace only — exact command names,
// flags, and arguments are intentionally omitted. Agents should run
// `devctl --help` to discover the live command surface.
func generateSkillContent() string {
	var sb strings.Builder
	now := time.Now().Format("2006-01-02")

	sb.WriteString("---\n")
	sb.WriteString("name: devctl-cli\n")
	sb.WriteString("description: Reference for all devctl CLI commands — services, sites, PHP, logs, mail, DNS, TLS, and SPX profiler management\n")
	sb.WriteString("compatibility: opencode\n")
	sb.WriteString("---\n\n")
	sb.WriteString("<!-- Auto-generated by `devctl devctl:skill` on " + now + " — do not edit manually -->\n\n")

	sb.WriteString("devctl is a local PHP dev environment CLI. All commands proxy to the running\n")
	sb.WriteString("daemon — no root required. Run `devctl --help` to see every command and its flags.\n\n")

	// Collect one-line descriptions per namespace by concatenating all command
	// descriptions into a readable capability summary.
	groups := map[string][]string{}
	var namespaces []string
	for _, cmd := range All() {
		parts := strings.SplitN(cmd.Name, ":", 2)
		ns := parts[0]
		if _, ok := groups[ns]; !ok {
			namespaces = append(namespaces, ns)
		}
		groups[ns] = append(groups[ns], cmd.Description)
	}

	nsOrder := []string{"services", "sites", "php", "logs", "dumps", "spx", "mail", "dns", "tls", "settings", "devctl"}
	seen := map[string]bool{}
	var ordered []string
	for _, ns := range nsOrder {
		if _, ok := groups[ns]; ok {
			ordered = append(ordered, ns)
			seen[ns] = true
		}
	}
	for _, ns := range namespaces {
		if !seen[ns] {
			ordered = append(ordered, ns)
		}
	}

	sb.WriteString("## Capabilities\n\n")
	for _, ns := range ordered {
		sb.WriteString(fmt.Sprintf("**%s** — ", ns))
		sb.WriteString(strings.Join(groups[ns], "; ") + ".\n\n")
	}

	sb.WriteString("## Usage\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString("devctl --help               # list all commands\n")
	sb.WriteString("devctl <command> --help     # flags for a specific command\n")
	sb.WriteString("devctl <command> --json     # machine-readable output\n")
	sb.WriteString("```\n")

	return sb.String()
}

func userHome() (string, error) {
	// Prefer SUDO_USER's home when running under sudo
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		// Look up the real user's home
		homeBase := "/home/" + sudoUser
		if _, err := os.Stat(homeBase); err == nil {
			return homeBase, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home, nil
}
