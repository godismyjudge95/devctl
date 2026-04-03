package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func init() {
	Register(&Cmd{
		Name:        "spx:profiles",
		Description: "List recent SPX profiler captures",
		Usage:       "[--domain=<domain>]",
		Flags:       []FlagDef{{Name: "domain", Description: "Filter by site domain"}},
		Examples: []string{
			"devctl spx:profiles",
			"devctl spx:profiles --domain=myapp.test",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("spx:profiles", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			domain := fs.String("domain", "", "filter by site domain")
			if err := fs.Parse(args); err != nil {
				return err
			}

			profiles, err := c.ListSPXProfiles(*domain)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(profiles)
				return nil
			}
			if len(profiles) == 0 {
				if *domain != "" {
					fmt.Printf("No SPX profiles for %s.\n", *domain)
				} else {
					fmt.Println("No SPX profiles found.")
				}
				return nil
			}
			var rows [][]string
			for _, p := range profiles {
				memMB := fmt.Sprintf("%.1f MB", float64(p.PeakMemoryBytes)/1024/1024)
				wallMs := fmt.Sprintf("%.1f ms", p.WallTimeMs)
				ts := time.Unix(p.Timestamp, 0).Format("Jan 02 15:04:05")
				rows = append(rows, []string{
					p.Key,
					p.Domain,
					p.Method + " " + p.URI,
					wallMs,
					memMB,
					ts,
				})
			}
			Table([]string{"Key", "Domain", "Endpoint", "Wall time", "Peak mem", "Captured"}, rows)
			fmt.Println()
			fmt.Println(styleDim.Render("Use `devctl spx:profile <key>` to see function hotspots."))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "spx:profile",
		Description: "Show CPU hotspot functions for an SPX profile",
		Usage:       "<key>",
		Args:        []ArgDef{{Name: "key", Description: "Profile key from spx:profiles"}},
		Examples:    []string{"devctl spx:profile abc123"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl spx:profile <key>")
			}
			key := args[0]
			detail, err := c.GetSPXProfileDetail(key)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(detail)
				return nil
			}
			memMB := float64(detail.PeakMemoryBytes) / 1024 / 1024
			Header(fmt.Sprintf("%s %s → %s", detail.Method, detail.URI, detail.Domain))
			KV("PHP version", detail.PHPVersion)
			KV("Wall time", fmt.Sprintf("%.1f ms", detail.WallTimeMs))
			KV("Peak memory", fmt.Sprintf("%.1f MB", memMB))
			KV("Functions called", fmt.Sprintf("%d", detail.CalledFuncCount))
			fmt.Println()

			if len(detail.Functions) == 0 {
				fmt.Println("No function data available.")
				return nil
			}
			limit := len(detail.Functions)
			if limit > 15 {
				limit = 15
			}

			var rows [][]string
			for _, f := range detail.Functions[:limit] {
				name := f.Name
				if len(name) > 55 {
					name = "…" + name[len(name)-54:]
				}
				rows = append(rows, []string{
					name,
					fmt.Sprintf("%d", f.Calls),
					fmt.Sprintf("%.2f ms", f.ExclusiveMs),
					fmt.Sprintf("%.1f%%", f.ExclusivePct),
				})
			}
			fmt.Printf("Top %d functions by exclusive CPU time:\n\n", limit)
			Table([]string{"Function", "Calls", "Excl ms", "Excl %"}, rows)

			if len(detail.Functions) > 15 {
				fmt.Println()
				fmt.Println(styleDim.Render(fmt.Sprintf("(showing 15 of %d functions)", len(detail.Functions))))
			}

			fmt.Println()
			fmt.Println(styleDim.Render("View flamegraph at http://127.0.0.1:4000 → Profiler tab"))

			// Check for common framework overhead patterns
			var suspects []string
			for _, f := range detail.Functions {
				if strings.Contains(f.Name, "Illuminate\\") && f.ExclusivePct > 5 {
					suspects = append(suspects, f.Name)
				}
			}
			_ = suspects
			return nil
		},
	})
}
