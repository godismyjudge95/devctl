package cli

import "fmt"

func init() {
	Register(&Cmd{
		Name:        "devctl:update",
		Description: "Check for a newer devctl release and update if one is available",
		Examples:    []string{"devctl devctl:update", "devctl devctl:update --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			status, err := c.GetSelfUpdateStatus()
			if err != nil {
				return err
			}

			if status.LatestVersion == "" {
				if jsonMode {
					PrintJSON(map[string]string{"status": "unknown", "message": "version check not yet complete, try again in a moment"})
					return nil
				}
				fmt.Println(styleDim.Render("Version check not yet complete — try again in a moment."))
				return nil
			}

			if !status.UpdateAvailable {
				if jsonMode {
					PrintJSON(map[string]any{"status": "up_to_date", "current_version": status.CurrentVersion})
					return nil
				}
				PrintOK("Already up to date (" + status.CurrentVersion + ")")
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
