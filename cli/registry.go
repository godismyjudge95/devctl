package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

const defaultAddr = "127.0.0.1:4000"

// ArgDef describes a positional argument for a command.
type ArgDef struct {
	Name        string
	Description string
	Optional    bool
}

// FlagDef describes a flag for a command.
type FlagDef struct {
	Name        string
	Default     string
	Description string
}

// Cmd is a single CLI command entry.
type Cmd struct {
	// Full name including namespace, e.g. "services:restart"
	Name string
	// Short one-line description
	Description string
	// Usage line after the command name, e.g. "<service-id>"
	Usage string
	// Positional args documentation
	Args []ArgDef
	// Flag documentation (for skill generation; actual parsing is in Handler)
	Flags []FlagDef
	// Examples shown in help and skill
	Examples []string
	// Handler is called with (client, args, jsonFlag).
	// args contains everything after the command name (before any flags were
	// stripped by the caller). jsonFlag indicates --json was set.
	Handler func(c *Client, args []string, jsonMode bool) error
}

// Registry holds all registered commands.
type Registry struct {
	cmds []*Cmd
}

var globalRegistry = &Registry{}

// Register adds a command to the global registry.
func Register(cmd *Cmd) {
	globalRegistry.cmds = append(globalRegistry.cmds, cmd)
}

// All returns all registered commands sorted by name.
func All() []*Cmd {
	out := make([]*Cmd, len(globalRegistry.cmds))
	copy(out, globalRegistry.cmds)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Find looks up a command by exact name.
func Find(name string) *Cmd {
	for _, c := range globalRegistry.cmds {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Dispatch is the CLI entry point. args is os.Args[1:].
// Returns true if a CLI command was handled (even if it errored).
func Dispatch(args []string) bool {
	if len(args) == 0 {
		return false
	}

	token := args[0]

	// Only handle tokens that look like CLI commands:
	//   - contain a colon           (services:list, services:, services:res)
	//   - are an exact namespace    (services, sites, php, …)
	//   - are a namespace prefix    (serv → services)
	//   - are "help"
	if token != "help" && !strings.Contains(token, ":") {
		if !looksLikeCLIToken(token) {
			return false
		}
	}

	// Parse global flags: --addr, --json, --help
	fs := flag.NewFlagSet("devctl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", addrFromEnv(), "devctl daemon address (default: $DEVCTL_ADDR or 127.0.0.1:4000)")
	jsonMode := fs.Bool("json", false, "output JSON instead of formatted text")

	// Separate the command name from the rest, then parse flags from the remainder
	cmdName := token
	rest := args[1:]

	if err := fs.Parse(rest); err != nil {
		if err == flag.ErrHelp {
			return true
		}
		PrintErr(fmt.Errorf("%v", err))
		os.Exit(1)
	}
	remaining := fs.Args()

	if cmdName == "help" {
		printGlobalHelp()
		return true
	}

	// --- Exact command match ---
	cmd := Find(cmdName)
	if cmd != nil {
		// Check for --help on the command itself
		for _, a := range remaining {
			if a == "--help" || a == "-h" {
				printCmdHelp(cmd)
				return true
			}
		}
		c := NewClient(*addr)
		if err := cmd.Handler(c, remaining, *jsonMode); err != nil {
			PrintErr(err)
			os.Exit(1)
		}
		return true
	}

	// --- Suggestion mode ---
	// Strip trailing colon so "services:" and "services" are equivalent.
	query := strings.TrimSuffix(cmdName, ":")

	if suggestions := suggestCommands(query); len(suggestions) > 0 {
		printSuggestions(query, suggestions)
		os.Exit(1)
	}

	// Nothing matched at all — let main.go print the unknown-command error.
	return false
}

// looksLikeCLIToken reports whether s is an exact namespace name or a prefix
// of one. This lets bare words like "services" or "serv" be caught by Dispatch
// instead of falling through to main.go's unknown-command handler.
func looksLikeCLIToken(s string) bool {
	for ns := range namespaces() {
		if ns == s || strings.HasPrefix(ns, s) || strings.HasPrefix(s, ns) {
			return true
		}
	}
	return false
}

// namespaces returns a set of all namespace prefixes present in the registry.
func namespaces() map[string]struct{} {
	ns := map[string]struct{}{}
	for _, cmd := range All() {
		parts := strings.SplitN(cmd.Name, ":", 2)
		ns[parts[0]] = struct{}{}
	}
	return ns
}

// suggestCommands returns commands that match query by:
//  1. Exact namespace match      ("services"   → all services:* commands)
//  2. Prefix match on namespace  ("serv"        → all services:* commands)
//  3. Prefix match on full name  ("services:re" → services:restart)
func suggestCommands(query string) []*Cmd {
	all := All()
	var out []*Cmd

	for _, cmd := range all {
		parts := strings.SplitN(cmd.Name, ":", 2)
		ns := parts[0]

		switch {
		// Exact or trailing-colon namespace: "services" or "services:"
		case query == ns:
			out = append(out, cmd)
		// Namespace prefix: "serv" matches "services"
		case strings.HasPrefix(ns, query):
			out = append(out, cmd)
		// Full-command prefix: "services:res" matches "services:restart"
		case strings.HasPrefix(cmd.Name, query):
			out = append(out, cmd)
		}
	}
	return out
}

// printSuggestions prints matched commands grouped by namespace.
func printSuggestions(query string, cmds []*Cmd) {
	fmt.Println()
	fmt.Fprintf(os.Stderr, "%s\n\n", styleDim.Render("Did you mean one of these?"))

	// Group by namespace
	groups := map[string][]*Cmd{}
	var order []string
	for _, cmd := range cmds {
		parts := strings.SplitN(cmd.Name, ":", 2)
		ns := parts[0]
		if _, ok := groups[ns]; !ok {
			order = append(order, ns)
		}
		groups[ns] = append(groups[ns], cmd)
	}

	for _, ns := range order {
		fmt.Fprintf(os.Stderr, "%s\n", styleLabel.Render(ns+":"))
		for _, cmd := range groups[ns] {
			name := styleBold.Render(cmd.Name)
			usage := ""
			if cmd.Usage != "" {
				usage = " " + styleDim.Render(cmd.Usage)
			}
			fmt.Fprintf(os.Stderr, "  %-42s %s\n", name+usage, cmd.Description)
		}
		fmt.Fprintln(os.Stderr)
	}
}

func addrFromEnv() string {
	if v := os.Getenv("DEVCTL_ADDR"); v != "" {
		return v
	}
	return defaultAddr
}

// PrintHelp prints the unified help output covering both system subcommands
// and all registered CLI commands. Called by main.go for --help and no-args.
func PrintHelp() {
	printGlobalHelp()
}

func printGlobalHelp() {
	fmt.Println()
	fmt.Println(styleHeader.Render("devctl — local PHP dev environment"))
	fmt.Println()
	fmt.Println(styleDim.Render("Usage:") + "  devctl <command> [args] [flags]")
	fmt.Println()

	// System subcommands (not in the CLI registry)
	fmt.Println(styleLabel.Render("system:"))
	type sysCmd struct{ name, usage, desc string }
	sysCmds := []sysCmd{
		{"daemon", "", "Start the devctl daemon (requires root)"},
		{"install", "[flags]", "Install devctl as a systemd service"},
		{"uninstall", "[--yes]", "Remove the devctl systemd service"},
		{"open", "", "Open the current project's .test URL in the browser"},
		{"version", "", "Print the version and exit"},
	}
	for _, c := range sysCmds {
		name := styleBold.Render(c.name)
		usage := ""
		if c.usage != "" {
			usage = " " + styleDim.Render(c.usage)
		}
		fmt.Printf("  %-42s %s\n", name+usage, c.desc)
	}
	fmt.Println()

	// Group CLI commands by namespace
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
	sort.Strings(namespaces)

	for _, ns := range namespaces {
		fmt.Println(styleLabel.Render(ns + ":"))
		for _, cmd := range groups[ns] {
			name := styleBold.Render(cmd.Name)
			usage := ""
			if cmd.Usage != "" {
				usage = " " + styleDim.Render(cmd.Usage)
			}
			fmt.Printf("  %-42s %s\n", name+usage, cmd.Description)
		}
		fmt.Println()
	}

	fmt.Println(styleDim.Render("CLI flags (for namespaced commands):"))
	fmt.Println("  --json              output raw JSON")
	fmt.Println("  --addr=host:port    daemon address (default: 127.0.0.1:4000)")
	fmt.Println("  --help              show help for a specific command")
	fmt.Println()
	fmt.Println(styleDim.Render("install flags:"))
	fmt.Println("  --user              non-root user devctl will manage (auto-detected from SUDO_USER)")
	fmt.Println("  --sites-dir         directory where sites are stored (default: ~/sites)")
	fmt.Println("  --path              directory to install the devctl binary into")
	fmt.Println("  --yes               skip all confirmation prompts")
	fmt.Println()
	fmt.Println(styleDim.Render("Examples:"))
	fmt.Println("  devctl services:list")
	fmt.Println("  devctl services:restart caddy")
	fmt.Println("  devctl sites:list --json")
	fmt.Println("  devctl logs:tail caddy")
	fmt.Println()
}

func printCmdHelp(cmd *Cmd) {
	fmt.Println()
	fmt.Printf("%s  %s\n\n", styleBold.Render("devctl "+cmd.Name), styleDim.Render(cmd.Usage))
	fmt.Println(cmd.Description)
	if len(cmd.Args) > 0 {
		fmt.Println()
		fmt.Println(styleDim.Render("Arguments:"))
		for _, a := range cmd.Args {
			opt := ""
			if a.Optional {
				opt = " (optional)"
			}
			fmt.Printf("  %-20s %s%s\n", styleLabel.Render(a.Name), a.Description, styleDim.Render(opt))
		}
	}
	if len(cmd.Flags) > 0 {
		fmt.Println()
		fmt.Println(styleDim.Render("Flags:"))
		for _, f := range cmd.Flags {
			def := ""
			if f.Default != "" {
				def = " (default: " + f.Default + ")"
			}
			fmt.Printf("  --%-18s %s%s\n", f.Name, f.Description, styleDim.Render(def))
		}
	}
	if len(cmd.Examples) > 0 {
		fmt.Println()
		fmt.Println(styleDim.Render("Examples:"))
		for _, e := range cmd.Examples {
			fmt.Println("  " + e)
		}
	}
	fmt.Println()
}
