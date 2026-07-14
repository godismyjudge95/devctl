package cli

import (
	"fmt"
	"strings"
)

func init() {
	Register(&Cmd{
		Name:        "tls:trust",
		Description: "Trust Caddy's internal CA in the system and browser certificate stores",
		Examples:    []string{"devctl tls:trust"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			output, err := c.TrustCA()
			if err != nil {
				// Prefer the typed elevate path when the daemon is non-root.
				if strings.Contains(err.Error(), "needs elevation") || strings.Contains(err.Error(), "elevate") {
					return fmt.Errorf("%w\n\nRun: sudo devctl elevate trust", err)
				}
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "trusted", "output": output})
				return nil
			}
			PrintOK("Caddy's internal CA is now trusted — HTTPS *.test sites should show no warnings")
			if output != "" {
				fmt.Println()
				fmt.Println(styleDim.Render(output))
			}
			return nil
		},
	})
}
