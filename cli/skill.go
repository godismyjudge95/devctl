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
		Description: "Generate the agent skill and refresh the <devctl> block in AGENTS.md",
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
			if err := UpdateAgentsFile(); err != nil {
				return err
			}
			if jsonMode {
				agentsPath, _ := defaultAgentsPath()
				PrintJSON(map[string]string{"status": "ok", "path": outputPath, "agents": agentsPath})
				return nil
			}
			PrintOK("Skill written to " + outputPath)
			if agentsPath, err := defaultAgentsPath(); err == nil {
				if _, err := os.Stat(agentsPath); err == nil {
					PrintOK("Updated <devctl> block in " + agentsPath)
				}
			}
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

// UpdateSkillIfInstalled silently regenerates the skill file if it exists
// and refreshes the <devctl> block in AGENTS.md when that file is present.
// Safe to call in a goroutine on daemon startup.
func UpdateSkillIfInstalled() {
	path, err := DefaultSkillPath()
	if err == nil {
		if _, err := os.Stat(path); err == nil {
			_ = WriteSkill(path)
		}
	}
	_ = UpdateAgentsFile()
}

// generateSkillContent builds the SKILL.md content from the command registry.
func generateSkillContent() string {
	now := time.Now().Format("2006-01-02")
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: devctl-cli\n")
	sb.WriteString("description: \"Use this skill for local PHP sites, artisan, composer, mysql, psql, wp-cli, Redis/Valkey, Mailpit, logs, *.test URLs, and service credentials. Covers the devctl CLI (colon commands like sites:list and services:credentials), wrapper binaries, and the Caddy stack. Do not use DDEV, Sail, Herd, or Valet.\"\n")
	sb.WriteString("compatibility: opencode\n")
	sb.WriteString("---\n\n")
	sb.WriteString("<devctl>\n")
	sb.WriteString("<!-- Auto-generated by `devctl devctl:skill` on " + now + ". Do not edit inside this block. -->\n\n")
	sb.WriteString(generateEnvGuide())
	sb.WriteString("\n\n")
	sb.WriteString(generateCommandCatalog())
	sb.WriteString("</devctl>\n")
	return sb.String()
}

func generateEnvGuide() string {
	bin := serverBinDir()
	var sb strings.Builder
	sb.WriteString("# Local PHP environment (devctl)\n\n")
	sb.WriteString("The directory name `ddev` is not the DDEV product. Do not run `ddev` commands.\n")
	sb.WriteString("Do not use Laravel Sail, Laravel Herd, Valet, or Docker Compose for this PHP stack.\n\n")
	sb.WriteString("This machine uses **devctl**. Command names use a colon: `devctl sites:list`.\n")
	sb.WriteString("Space form (`devctl sites list`) is wrong.\n\n")
	sb.WriteString("`php` on PATH is the wrapper at `" + bin + "/php`. You may call that path directly.\n\n")
	sb.WriteString("## First commands\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString("devctl sites:list --json\n")
	sb.WriteString("devctl sites:get <domain>\n")
	sb.WriteString("devctl services:list --json\n")
	sb.WriteString("devctl services:credentials <service-id>\n")
	sb.WriteString("devctl logs:list\n")
	sb.WriteString("devctl logs:tail <log-id>\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Get database and Redis passwords with `devctl services:credentials`.\n")
	sb.WriteString("Do not invent host, port, or password.\n\n")
	sb.WriteString("## PHP and Artisan\n\n")
	sb.WriteString("Run Artisan from the application root (often `web/` in a monorepo).\n")
	sb.WriteString("Do not run `php artisan serve`. The site is already `https://<name>.test`.\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString("php artisan <command>\n")
	sb.WriteString(bin + "/php artisan <command>\n")
	sb.WriteString("```\n\n")
	sb.WriteString("## Database, Redis, WordPress\n\n")
	sb.WriteString("Use `" + bin + "/mysql`, `" + bin + "/psql`, and `" + bin + "/wp`.\n")
	sb.WriteString("Redis CLI is `valkey-cli`. `redis-cli` exists only if that symlink is present.\n\n")
	sb.WriteString("## Browser TLS\n\n")
	sb.WriteString("Caddy uses an internal CA. For `https://*.test`, ignore certificate errors.\n\n")
	sb.WriteString("## Never\n\n")
	sb.WriteString("- `ddev …`\n")
	sb.WriteString("- `php artisan serve`\n")
	sb.WriteString("- `./vendor/bin/sail`\n")
	sb.WriteString("- `*.ddev.site` URLs\n")
	return sb.String()
}

func generateCommandCatalog() string {
	groups := map[string][]*Cmd{}
	var namespaces []string
	for _, cmd := range All() {
		parts := strings.SplitN(cmd.Name, ":", 2)
		ns := parts[0]
		if _, ok := groups[ns]; !ok {
			namespaces = append(namespaces, ns)
		}
		groups[ns] = append(groups[ns], cmd)
	}

	nsOrder := []string{
		"services", "sites", "php", "logs", "dumps", "spx", "mail",
		"dns", "tls", "settings", "helpers", "postgres", "elevate", "devctl",
	}
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

	var sb strings.Builder
	sb.WriteString("## Global flags\n\n")
	sb.WriteString("- `--json` — machine-readable JSON output\n")
	sb.WriteString("- `--addr=host:port` — daemon address (default `127.0.0.1:4000` or `$DEVCTL_ADDR`)\n\n")
	sb.WriteString("Run `devctl <command> --help` only when a flag is missing from this list.\n\n")

	for _, ns := range ordered {
		sb.WriteString("## " + ns + ": commands\n\n")
		for _, cmd := range groups[ns] {
			usage := ""
			if cmd.Usage != "" {
				usage = " " + cmd.Usage
			}
			sb.WriteString(fmt.Sprintf("- `devctl %s%s` — %s\n", cmd.Name, usage, cmd.Description))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func serverBinDir() string {
	if r := strings.TrimSpace(os.Getenv("DEVCTL_SERVER_ROOT")); r != "" {
		return filepath.Join(r, "bin")
	}
	home, err := userHome()
	if err != nil {
		return filepath.Join("ddev", "sites", "server", "bin")
	}
	return filepath.Join(home, "ddev", "sites", "server", "bin")
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
