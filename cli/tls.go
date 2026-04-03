package cli

import "fmt"

func init() {
	Register(&Cmd{
		Name:        "tls:trust",
		Description: "Trust Caddy's internal CA in the system and browser certificate stores",
		Examples:    []string{"devctl tls:trust"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			output, err := c.TrustCA()
			if err != nil {
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
