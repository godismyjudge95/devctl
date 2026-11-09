package cli

import (
	"flag"
	"fmt"
	"os"
)

func init() {
	Register(&Cmd{
		Name:        "devctl:update",
		MCP:         MCPHidden,
		Description: "Check for a newer devctl release and update if one is available",
		Usage:       "[--check]",
		Flags:       []FlagDef{{Name: "check", Default: "false", Description: "Only check for a newer release. Do not install it"}},
		Examples:    []string{"devctl devctl:update", "devctl devctl:update --check", "devctl devctl:update --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("devctl:update", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			checkOnly := fs.Bool("check", false, "only check for a newer release")
			if err := fs.Parse(args); err != nil {
				return err
			}

			status, err := c.CheckSelfUpdate()
			if err != nil {
				return err
			}

			if !status.UpdateAvailable {
				if jsonMode {
					PrintJSON(map[string]any{"status": "up_to_date", "current_version": status.CurrentVersion})
					return nil
				}
				PrintOK("Already up to date (" + status.CurrentVersion + ")")
				return nil
			}

			if *checkOnly {
				if jsonMode {
					PrintJSON(map[string]any{"status": "update_available", "current_version": status.CurrentVersion, "latest_version": status.LatestVersion})
					return nil
				}
				fmt.Printf("devctl %s is available (installed: %s). Run devctl devctl:update to install it.\n", styleBold.Render(status.LatestVersion), styleDim.Render(status.CurrentVersion))
				return nil
			}

			if jsonMode {
				type outputEvent struct {
					Type string `json:"type"`
					Line string `json:"line,omitempty"`
				}
				err := c.ApplySelfUpdateSSE(func(line string) {
					PrintJSON(outputEvent{Type: "output", Line: line})
				})
				if err != nil {
					PrintJSON(map[string]string{"type": "error", "error": err.Error()})
					return err
				}
				PrintJSON(map[string]string{"type": "done", "version": status.LatestVersion})
				return nil
			}

			fmt.Printf("Updating devctl %s → %s…\n", styleDim.Render(status.CurrentVersion), styleBold.Render(status.LatestVersion))
			err = c.ApplySelfUpdateSSE(func(line string) {
				fmt.Println(styleDim.Render("  " + line))
			})
			if err != nil {
				return err
			}
			PrintOK("devctl updated to " + status.LatestVersion + " — restarting service…")
			return nil
		},
	})
}
