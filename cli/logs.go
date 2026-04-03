package cli

import (
	"flag"
	"fmt"
	"os"
)

func init() {
	Register(&Cmd{
		Name:        "logs:list",
		Description: "List available log files",
		Examples:    []string{"devctl logs:list", "devctl logs:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			logs, err := c.ListLogFiles()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(logs)
				return nil
			}
			if len(logs) == 0 {
				fmt.Println("No log files found.")
				return nil
			}
			var rows [][]string
			for _, l := range logs {
				size := fmt.Sprintf("%.1f KB", float64(l.Size)/1024)
				rows = append(rows, []string{l.ID, l.Name, size})
			}
			Table([]string{"ID", "Name", "Size"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "logs:tail",
		Description: "Show the tail of a log file",
		Usage:       "<log-id> [--bytes=N]",
		Args:        []ArgDef{{Name: "log-id", Description: "Log file ID (use logs:list to see IDs)"}},
		Flags:       []FlagDef{{Name: "bytes", Default: "16384", Description: "Number of bytes to read from end of file (max 131072)"}},
		Examples: []string{
			"devctl logs:tail caddy",
			"devctl logs:tail php-fpm-8.3 --bytes=32768",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("logs:tail", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			bytes := fs.Int("bytes", 16384, "bytes to read from end")
			if err := fs.Parse(args); err != nil {
				return err
			}
			remaining := fs.Args()
			if len(remaining) == 0 {
				return fmt.Errorf("usage: devctl logs:tail <log-id> [--bytes=N]")
			}
			logID := remaining[0]
			n := *bytes
			if n <= 0 {
				n = 16384
			}
			if n > 131072 {
				n = 131072
			}

			tail, err := c.GetLogTail(logID, n)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"log_id": logID, "content": tail})
				return nil
			}
			if tail == "" {
				fmt.Println(styleDim.Render("(log is empty)"))
				return nil
			}
			fmt.Print(tail)
			if len(tail) > 0 && tail[len(tail)-1] != '\n' {
				fmt.Println()
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "logs:clear",
		Description: "Clear (truncate) a log file",
		Usage:       "<log-id>",
		Args:        []ArgDef{{Name: "log-id", Description: "Log file ID (use logs:list to see IDs)"}},
		Examples:    []string{"devctl logs:clear caddy", "devctl logs:clear php-fpm-8.3"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl logs:clear <log-id>")
			}
			id := args[0]
			if err := c.ClearLog(id); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok", "log_id": id})
				return nil
			}
			PrintOK("Log " + id + " cleared")
			return nil
		},
	})
}
