package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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
		Usage:       "<log-id> [--bytes=N] [--follow]",
		Args:        []ArgDef{{Name: "log-id", Description: "Log file ID (use logs:list to see IDs)"}},
		Flags: []FlagDef{
			{Name: "bytes", Default: "16384", Description: "Number of bytes to read from end of file (max 131072)"},
			{Name: "follow", Default: "false", Description: "Follow the log output (like tail -f)"},
		},
		Examples: []string{
			"devctl logs:tail caddy",
			"devctl logs:tail php-fpm-8.3 --bytes=32768",
			"devctl logs:tail caddy --follow",
			"devctl logs:tail caddy -f",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("logs:tail", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			bytes := fs.Int("bytes", 16384, "bytes to read from end")
			follow := fs.Bool("follow", false, "follow the log output")
			// Support short -f flag.
			fs.BoolVar(follow, "f", false, "follow the log output (shorthand)")

			// Go's flag package stops parsing at the first non-flag argument,
			// so "logs:tail php-fpm-8.4 -f" would leave "-f" unparsed.
			// Fix: extract the positional log-id first (the first arg that does
			// not start with "-"), then pass only the remaining flag args to
			// fs.Parse so all flags are parsed regardless of their position.
			var logID string
			var flagArgs []string
			for _, a := range args {
				if logID == "" && len(a) > 0 && a[0] != '-' {
					logID = a
				} else {
					flagArgs = append(flagArgs, a)
				}
			}
			if logID == "" {
				return fmt.Errorf("usage: devctl logs:tail <log-id> [--bytes=N] [--follow]")
			}
			if err := fs.Parse(flagArgs); err != nil {
				return err
			}

			if *follow {
				// Live-follow mode: consume SSE stream, exit on Ctrl-C.
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer cancel()
				return c.StreamLog(ctx, logID, os.Stdout)
			}

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
