package cli

import "fmt"

func init() {
	Register(&Cmd{
		Name:        "services:list",
		Description: "List all managed services and their status",
		Examples:    []string{"devctl services:list", "devctl services:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			svcs, err := c.ListServices()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(svcs)
				return nil
			}
			var rows [][]string
			for _, s := range svcs {
				if !s.Installed {
					continue
				}
				ver := s.Version
				if ver == "" {
					ver = styleDim.Render("—")
				}
				update := ""
				if s.UpdateAvailable {
					update = styleWarn.Render(" ↑ " + s.LatestVersion)
				}
				rows = append(rows, []string{
					s.ID,
					s.Label,
					StatusStyle(s.Status),
					ver + update,
				})
			}
			if len(rows) == 0 {
				fmt.Println("No services installed.")
				return nil
			}
			Table([]string{"ID", "Label", "Status", "Version"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "services:available",
		Description: "List services that can be installed",
		Examples:    []string{"devctl services:available", "devctl services:available --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			svcs, err := c.ListServices()
			if err != nil {
				return err
			}

			// Filter: installable but not yet installed.
			var available []ServiceState
			for _, s := range svcs {
				if s.Installable && !s.Installed {
					available = append(available, s)
				}
			}

			if jsonMode {
				PrintJSON(available)
				return nil
			}

			if len(available) == 0 {
				fmt.Println("All installable services are already installed.")
				return nil
			}

			var rows [][]string
			for _, s := range available {
				rows = append(rows, []string{
					s.ID,
					s.Label,
					s.InstallVersion,
					s.Description,
				})
			}
			Table([]string{"ID", "Label", "Version", "Description"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "services:install",
		Description: "Install an available service",
		Usage:       "<service-id>",
		Args:        []ArgDef{{Name: "service-id", Description: "Service ID to install (e.g. mailpit, redis, postgres)"}},
		Examples:    []string{"devctl services:install mailpit", "devctl services:install postgres"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl services:install <service-id>")
			}
			id := args[0]

			if jsonMode {
				// In JSON mode, stream output lines as JSON objects and emit a
				// final status object.
				type outputEvent struct {
					Type string `json:"type"`
					Line string `json:"line,omitempty"`
				}
				err := c.InstallServiceSSE(id, func(line string) {
					PrintJSON(outputEvent{Type: "output", Line: line})
				})
				if err != nil {
					PrintJSON(map[string]string{"type": "error", "error": err.Error()})
					return err
				}
				PrintJSON(map[string]string{"type": "done", "service_id": id})
				return nil
			}

			fmt.Printf("Installing %s…\n", styleBold.Render(id))
			err := c.InstallServiceSSE(id, func(line string) {
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
		Name:        "services:start",
		Description: "Start a stopped service",
		Usage:       "<service-id>",
		Args:        []ArgDef{{Name: "service-id", Description: "Service ID (e.g. redis, mailpit, php-fpm-8.3)"}},
		Examples:    []string{"devctl services:start redis", "devctl services:start php-fpm-8.3"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl services:start <service-id>")
			}
			id := args[0]
			if err := c.StartService(id); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "service_id": id})
				return nil
			}
			PrintOK("Start command sent to " + id)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "services:stop",
		Description: "Stop a running service",
		Usage:       "<service-id>",
		Args:        []ArgDef{{Name: "service-id", Description: "Service ID (e.g. redis, mailpit)"}},
		Examples:    []string{"devctl services:stop redis"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl services:stop <service-id>")
			}
			id := args[0]
			if err := c.StopService(id); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "service_id": id})
				return nil
			}
			PrintOK("Stop command sent to " + id)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "services:restart",
		Description: "Restart a service",
		Usage:       "<service-id>",
		Args:        []ArgDef{{Name: "service-id", Description: "Service ID (e.g. caddy, redis, php-fpm-8.3)"}},
		Examples:    []string{"devctl services:restart caddy", "devctl services:restart php-fpm-8.3"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl services:restart <service-id>")
			}
			id := args[0]
			if err := c.RestartService(id); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "service_id": id})
				return nil
			}
			PrintOK("Restart command sent to " + id)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "services:credentials",
		Description: "Show connection credentials for a service",
		Usage:       "<service-id>",
		Args:        []ArgDef{{Name: "service-id", Description: "Service ID (e.g. postgres, mysql)"}},
		Examples:    []string{"devctl services:credentials postgres", "devctl services:credentials mysql"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl services:credentials <service-id>")
			}
			id := args[0]
			creds, err := c.GetServiceCredentials(id)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(creds)
				return nil
			}
			if len(creds) == 0 {
				fmt.Println("No credentials found for " + id)
				return nil
			}
			Header("Credentials: " + id)
			for k, v := range creds {
				KV(k, v)
			}
			return nil
		},
	})
}
