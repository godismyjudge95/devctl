package cli

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func init() {
	Register(&Cmd{
		Name:        "dumps:list",
		Description: "List recent php_dd() variable dumps",
		Usage:       "[--domain=<domain>]",
		Flags:       []FlagDef{{Name: "domain", Description: "Filter by site domain"}},
		Examples: []string{
			"devctl dumps:list",
			"devctl dumps:list --domain=myapp.test",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("dumps:list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			domain := fs.String("domain", "", "filter by site domain")
			if err := fs.Parse(args); err != nil {
				return err
			}

			dumps, err := c.ListDumps(*domain)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(dumps)
				return nil
			}
			if len(dumps) == 0 {
				if *domain != "" {
					fmt.Printf("No dumps for %s.\n", *domain)
				} else {
					fmt.Println("No dumps found.")
				}
				return nil
			}
			fmt.Printf("%s\n\n", styleBold.Render(fmt.Sprintf("%d dump(s)", len(dumps))))
			for _, d := range dumps {
				file := "unknown"
				if d.File != nil {
					file = *d.File
				}
				line := int64(0)
				if d.Line != nil {
					line = *d.Line
				}
				site := "unknown"
				if d.SiteDomain != nil {
					site = *d.SiteDomain
				}
				ts := time.Unix(int64(d.Timestamp), 0).Format("15:04:05")
				fmt.Printf("%s  %s  %s\n",
					styleDim.Render(fmt.Sprintf("#%d", d.ID)),
					styleBold.Render(fmt.Sprintf("%s:%d", file, line)),
					styleDim.Render("("+site+" @ "+ts+")"),
				)
				nodes := d.Nodes
				if len(nodes) > 400 {
					nodes = nodes[:400] + "…"
				}
				fmt.Printf("  %s\n\n", nodes)
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "dumps:clear",
		Description: "Delete all php_dd() dumps",
		Examples:    []string{"devctl dumps:clear"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if err := c.ClearDumps(); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok"})
				return nil
			}
			PrintOK("All dumps cleared")
			return nil
		},
	})
}
