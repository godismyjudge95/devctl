package cli

import (
	"fmt"
	"strings"
)

func init() {
	Register(&Cmd{
		Name:        "node:list",
		Description: "List installed Node.js versions and the sites that use each",
		Examples:    []string{"devctl node:list", "devctl node:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			state, err := c.GetNode()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(state)
				return nil
			}
			if len(state.Versions) == 0 {
				fmt.Println("No Node versions installed. Run: devctl node:install lts")
				return nil
			}
			var rows [][]string
			for _, v := range state.Versions {
				def := ""
				if v.Default {
					def = "default"
				}
				sites := strings.Join(v.Sites, ", ")
				if sites == "" {
					sites = styleDim.Render("—")
				}
				rows = append(rows, []string{v.Version, def, sites})
			}
			Table([]string{"Version", "Default", "Sites"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "node:install",
		Description: "Install a Node.js version with fnm (installs fnm first when missing)",
		Usage:       "<version>",
		Args:        []ArgDef{{Name: "version", Description: "22, 20.11.1, lts, lts/<name>, or latest"}},
		Examples:    []string{"devctl node:install 22", "devctl node:install lts", "devctl node:install 20 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl node:install <version>")
			}
			version := args[0]
			if jsonMode {
				type outputEvent struct {
					Type string `json:"type"`
					Line string `json:"line,omitempty"`
				}
				err := c.InstallNodeSSE(version, func(line string) {
					PrintJSON(outputEvent{Type: "output", Line: line})
				})
				if err != nil {
					PrintJSON(map[string]string{"type": "error", "error": err.Error()})
					return err
				}
				PrintJSON(map[string]string{"type": "done", "version": version})
				return nil
			}
			fmt.Printf("Installing Node %s…\n", styleBold.Render(version))
			err := c.InstallNodeSSE(version, func(line string) {
				fmt.Println(styleDim.Render("  " + strings.TrimRight(line, "\n")))
			})
			if err != nil {
				return err
			}
			PrintOK("Node " + version + " installed")
			return nil
		},
	})

	Register(&Cmd{
		Name:        "node:uninstall",
		Description: "Remove an installed Node.js version",
		Usage:       "<version>",
		Args:        []ArgDef{{Name: "version", Description: "Exact installed version from node:list (e.g. v22.11.0)"}},
		Examples:    []string{"devctl node:uninstall v22.11.0", "devctl node:uninstall 20.11.1 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl node:uninstall <version>")
			}
			version := "v" + strings.TrimPrefix(args[0], "v")
			if err := c.UninstallNode(version); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "version": version})
				return nil
			}
			PrintOK("Node " + version + " uninstalled")
			return nil
		},
	})
}
