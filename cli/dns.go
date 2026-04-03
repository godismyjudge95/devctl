package cli

import "fmt"

func init() {
	Register(&Cmd{
		Name:        "dns:status",
		Description: "Check whether systemd-resolved is configured for *.test",
		Examples:    []string{"devctl dns:status"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			configured, err := c.CheckDNSSetup()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]bool{"configured": configured})
				return nil
			}
			if configured {
				PrintOK("DNS is configured — *.test domains resolve to this machine")
			} else {
				fmt.Println(styleWarn.Render("⚠ DNS is NOT configured — *.test domains will not resolve"))
				fmt.Println()
				fmt.Println("Run `devctl dns:setup` to configure.")
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "dns:setup",
		Description: "Configure systemd-resolved to route *.test queries to devctl",
		Examples:    []string{"devctl dns:setup"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if err := c.ConfigureDNS(); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "configured"})
				return nil
			}
			PrintOK("DNS configured — systemd-resolved now routes *.test to devctl")
			return nil
		},
	})

	Register(&Cmd{
		Name:        "dns:teardown",
		Description: "Remove the systemd-resolved *.test DNS configuration",
		Examples:    []string{"devctl dns:teardown"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if err := c.TeardownDNS(); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "removed"})
				return nil
			}
			PrintOK("DNS teardown complete — *.test domains will no longer resolve")
			return nil
		},
	})
}
