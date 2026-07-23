package cli

import (
	"fmt"
)

func init() {
	Register(&Cmd{
		Name:        "postgres:extensions",
		Description: "List managed PostgreSQL extensions and their status",
		Examples:    []string{"devctl postgres:extensions", "devctl postgres:extensions --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			exts, err := c.ListPostgresExtensions()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(exts)
				return nil
			}
			if len(exts) == 0 {
				PrintOK("No managed PostgreSQL extensions")
				return nil
			}
			rows := make([][]string, 0, len(exts))
			for _, e := range exts {
				ready := "no"
				if e.Ready {
					ready = "yes"
				}
				files := "no"
				if e.FilesInstalled {
					files = "yes"
				}
				wired := "no"
				if e.Wired {
					wired = "yes"
				}
				rows = append(rows, []string{
					e.ID,
					e.Version,
					files,
					wired,
					ready,
					e.Note,
				})
			}
			Table([]string{"ID", "Version", "Files", "Wired", "Ready", "Note"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "postgres:extensions:ensure",
		Description: "Install missing managed extension files and wire SQL objects",
		Examples:    []string{"devctl postgres:extensions:ensure", "devctl postgres:extensions:ensure --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			res, err := c.EnsurePostgresExtensions()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(res)
				return nil
			}
			status, _ := res["status"].(string)
			if status == "ok" {
				PrintOK("PostgreSQL extensions ensured")
			} else {
				errMsg, _ := res["error"].(string)
				fmt.Println(styleWarn.Render("partial: " + errMsg))
			}
			// Print list after ensure.
			exts, err := c.ListPostgresExtensions()
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(exts))
			for _, e := range exts {
				ready := "no"
				if e.Ready {
					ready = "yes"
				}
				rows = append(rows, []string{e.ID, e.Version, ready, e.Note})
			}
			Table([]string{"ID", "Version", "Ready", "Note"}, rows)
			return nil
		},
	})
}
