package cli

import (
	"fmt"
)

var settingsKeys = []string{
	"devctl_host",
	"devctl_port",
	"dump_tcp_port",
	"service_poll_interval",
	"mailpit_http_port",
	"mailpit_smtp_port",
	"dns_port",
	"dns_target_ip",
	"dns_tld",
}

func init() {
	Register(&Cmd{
		Name:        "settings:get",
		Description: "Show all devctl settings",
		Examples:    []string{"devctl settings:get", "devctl settings:get --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			settings, err := c.GetSettings()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(settings)
				return nil
			}
			Header("devctl Settings")
			for _, k := range settingsKeys {
				if v, ok := settings[k]; ok {
					KV(k, v)
				}
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "settings:set",
		Description: "Update devctl settings",
		Usage:       "<key=value>...",
		Args: []ArgDef{{
			Name:        "key=value",
			Description: "Settings to change: devctl_host, devctl_port, dump_tcp_port, service_poll_interval, mailpit_http_port, mailpit_smtp_port, dns_port, dns_target_ip, dns_tld",
		}},
		Examples: []string{
			"devctl settings:set devctl_port=4001",
			"devctl settings:set dns_tld=local mailpit_http_port=9025",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl settings:set <key=value>...\n  valid keys: %v", settingsKeys)
			}

			kv := parseKV(args)
			if len(kv) == 0 {
				return fmt.Errorf("no valid key=value pairs found in arguments")
			}

			// Validate keys
			valid := map[string]bool{}
			for _, k := range settingsKeys {
				valid[k] = true
			}
			for k := range kv {
				if !valid[k] {
					return fmt.Errorf("unknown setting %q — valid keys: %v", k, settingsKeys)
				}
			}

			if err := c.SetSettings(kv); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"status": "ok", "updated": kv})
				return nil
			}
			PrintOK("Settings updated")
			for k, v := range kv {
				KV(k, v)
			}
			fmt.Println()
			fmt.Println(styleDim.Render("Some changes (e.g. port) require restarting the devctl service."))
			return nil
		},
	})
}
