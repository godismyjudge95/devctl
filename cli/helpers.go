package cli

import "fmt"

func init() {
	Register(&Cmd{
		Name:        "helpers:list",
		Description: "List installed CLI helpers and their versions",
		Examples:    []string{"devctl helpers:list", "devctl helpers:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			helpers, err := c.ListHelpers()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(helpers)
				return nil
			}
			var rows [][]string
			for _, h := range helpers {
				if !h.Installed {
					continue
				}
				ver := h.Version
				if ver == "" {
					ver = styleDim.Render("—")
				}
				if h.UpdateAvailable {
					ver += styleWarn.Render(" ↑ " + h.LatestVersion)
				}
				rows = append(rows, []string{h.ID, h.Label, ver})
			}
			if len(rows) == 0 {
				fmt.Println("No helpers installed.")
				return nil
			}
			Table([]string{"ID", "Label", "Version"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "helpers:available",
		Description: "List CLI helpers that can be installed",
		Examples:    []string{"devctl helpers:available", "devctl helpers:available --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			helpers, err := c.ListHelpers()
			if err != nil {
				return err
			}
			var available []HelperState
			for _, h := range helpers {
				if !h.Installed {
					available = append(available, h)
				}
			}
			if jsonMode {
				PrintJSON(available)
				return nil
			}
			if len(available) == 0 {
				fmt.Println("All helpers are already installed.")
				return nil
			}
			var rows [][]string
			for _, h := range available {
				rows = append(rows, []string{h.ID, h.Label, h.Description})
			}
			Table([]string{"ID", "Label", "Description"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "helpers:install",
		Description: "Download and install a CLI helper",
		Usage:       "<helper-id>",
		Args:        []ArgDef{{Name: "helper-id", Description: "Helper ID (e.g. mago, phpantom_lsp, wp)"}},
		Examples:    []string{"devctl helpers:install mago", "devctl helpers:install wp"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl helpers:install <helper-id>")
			}
			id := args[0]
			if jsonMode {
				type outputEvent struct {
					Type string `json:"type"`
					Line string `json:"line,omitempty"`
				}
				err := c.InstallHelperSSE(id, func(line string) {
					PrintJSON(outputEvent{Type: "output", Line: line})
				})
				if err != nil {
					PrintJSON(map[string]string{"type": "error", "error": err.Error()})
					return err
				}
				PrintJSON(map[string]string{"type": "done", "id": id})
				return nil
			}
			fmt.Printf("Installing %s…\n", styleBold.Render(id))
			err := c.InstallHelperSSE(id, func(line string) {
				fmt.Println(styleDim.Render("  " + line))
			})
			if err != nil {
				return err
			}
			PrintOK(id + " installed")
			return nil
		},
	})

	Register(&Cmd{
		Name:        "helpers:update",
		Description: "Update an installed CLI helper to the latest version",
		Usage:       "<helper-id>",
		Args:        []ArgDef{{Name: "helper-id", Description: "Helper ID (e.g. mago, sqlite3)"}},
		Examples:    []string{"devctl helpers:update mago"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl helpers:update <helper-id>")
			}
			id := args[0]
			if jsonMode {
				type outputEvent struct {
					Type string `json:"type"`
					Line string `json:"line,omitempty"`
				}
				err := c.UpdateHelperSSE(id, func(line string) {
					PrintJSON(outputEvent{Type: "output", Line: line})
				})
				if err != nil {
					PrintJSON(map[string]string{"type": "error", "error": err.Error()})
					return err
				}
				PrintJSON(map[string]string{"type": "done", "id": id})
				return nil
			}
			fmt.Printf("Updating %s…\n", styleBold.Render(id))
			err := c.UpdateHelperSSE(id, func(line string) {
				fmt.Println(styleDim.Render("  " + line))
			})
			if err != nil {
				return err
			}
			PrintOK(id + " updated")
			return nil
		},
	})

	Register(&Cmd{
		Name:        "helpers:uninstall",
		Description: "Remove an installed CLI helper",
		Usage:       "<helper-id>",
		Args:        []ArgDef{{Name: "helper-id", Description: "Helper ID (e.g. mago, fnm)"}},
		Examples:    []string{"devctl helpers:uninstall mago"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl helpers:uninstall <helper-id>")
			}
			id := args[0]
			if err := c.UninstallHelper(id); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "id": id})
				return nil
			}
			PrintOK(id + " uninstalled")
			return nil
		},
	})
}
