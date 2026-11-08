package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

// findWorker resolves "<domain> <type|id>" to one worker of that site.
func findWorker(c *Client, domain, ref string) (Worker, error) {
	site, err := findSite(c, domain, nil)
	if err != nil {
		return Worker{}, err
	}
	list, err := c.ListWorkers(site.ID)
	if err != nil {
		return Worker{}, err
	}
	id, idErr := strconv.ParseInt(ref, 10, 64)
	var matches []Worker
	for _, w := range list {
		if (idErr == nil && w.ID == id) || w.Type == ref {
			matches = append(matches, w)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Worker{}, fmt.Errorf("%s has no %s worker — run `devctl workers:list %s`", site.Domain, ref, site.Domain)
	}
	ids := make([]string, len(matches))
	for i, w := range matches {
		ids[i] = strconv.FormatInt(w.ID, 10)
	}
	return Worker{}, fmt.Errorf("%s has %d %s workers; use the id (%s)", site.Domain, len(matches), ref, strings.Join(ids, ", "))
}

func printWorker(w Worker, jsonMode bool, verb string) error {
	if jsonMode {
		PrintJSON(w)
	} else if w.Status != "failed" {
		PrintOK(fmt.Sprintf("%s %s worker %d %s (%s)", w.Domain, w.Type, w.ID, verb, w.Status))
	}
	if w.Status == "failed" {
		return fmt.Errorf("%s %s worker %d failed to start: %s", w.Domain, w.Type, w.ID, w.Error)
	}
	return nil
}

// workerActionCmd builds workers:start, workers:stop, and workers:restart.
func workerActionCmd(action, verb, description string) *Cmd {
	name := "workers:" + action
	return &Cmd{
		Name:        name,
		Description: description,
		Usage:       "<domain> <type|id>",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain, id, or path (. for the cwd)"},
			{Name: "type|id", Description: "Worker type (queue, horizon, schedule, vite) or worker id"},
		},
		Examples: []string{"devctl " + name + " myapp.test queue", "devctl " + name + " myapp.test 7 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: devctl %s <domain> <type|id>", name)
			}
			w, err := findWorker(c, args[0], args[1])
			if err != nil {
				return err
			}
			w, err = c.WorkerAction(w.ID, action)
			if err != nil {
				return err
			}
			return printWorker(w, jsonMode, verb)
		},
	}
}

func init() {
	Register(&Cmd{
		Name:        "workers:list",
		Description: "List site workers (queue, horizon, schedule, vite, custom) and their status",
		Usage:       "[domain]",
		Args:        []ArgDef{{Name: "domain", Description: "Only this site (domain, id, or path)", Optional: true}},
		Examples:    []string{"devctl workers:list", "devctl workers:list myapp.test --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			siteID := ""
			if len(args) > 0 {
				site, err := findSite(c, args[0], nil)
				if err != nil {
					return err
				}
				siteID = site.ID
			}
			list, err := c.ListWorkers(siteID)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(list)
				return nil
			}
			if len(list) == 0 {
				fmt.Println("No workers. Add one: devctl workers:add <domain> queue")
				return nil
			}
			var rows [][]string
			for _, w := range list {
				status := StatusStyle(w.Status)
				if w.CrashLoop {
					status += styleWarn.Render(fmt.Sprintf(" (crashed %d times)", w.Crashes))
				}
				rows = append(rows, []string{strconv.FormatInt(w.ID, 10), w.Domain, w.Type, status, w.CommandLine})
			}
			Table([]string{"ID", "Site", "Type", "Status", "Command"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "workers:add",
		Description: "Add a worker to a site and start it",
		Usage:       "<domain> <type> [--command=...]",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain, id, or path (. for the cwd)"},
			{Name: "type", Description: "queue (artisan queue:work), horizon, schedule (artisan schedule:work), vite (npm run dev), or custom"},
		},
		Flags: []FlagDef{{Name: "command", Description: "Shell command for a custom worker; runs in the site root with the devctl bin dir on PATH"}},
		Examples: []string{
			"devctl workers:add myapp.test queue",
			"devctl workers:add myapp.test vite",
			`devctl workers:add myapp.test custom --command="php8.4 artisan reverb:start"`,
			"devctl workers:add . schedule --json",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("workers:add", flag.ContinueOnError)
			command := fs.String("command", "", "")
			pos, err := parseInterspersed(fs, args)
			if err != nil {
				return err
			}
			if len(pos) < 2 {
				return fmt.Errorf("usage: devctl workers:add <domain> <type> [--command=...]")
			}
			site, err := findSite(c, pos[0], nil)
			if err != nil {
				return err
			}
			w, err := c.AddWorker(site.ID, pos[1], *command)
			if err != nil {
				return err
			}
			return printWorker(w, jsonMode, "added")
		},
	})

	Register(&Cmd{
		Name:        "workers:remove",
		Description: "Stop a worker and remove it from the site",
		Usage:       "<domain> <type|id>",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain, id, or path (. for the cwd)"},
			{Name: "type|id", Description: "Worker type or worker id"},
		},
		Examples: []string{"devctl workers:remove myapp.test queue", "devctl workers:remove myapp.test 7 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: devctl workers:remove <domain> <type|id>")
			}
			w, err := findWorker(c, args[0], args[1])
			if err != nil {
				return err
			}
			if err := c.RemoveWorker(w.ID); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"status": "ok", "id": w.ID})
				return nil
			}
			PrintOK(fmt.Sprintf("%s %s worker %d removed", w.Domain, w.Type, w.ID))
			return nil
		},
	})

	Register(workerActionCmd("start", "started", "Start a worker (it also starts with devctl from now on)"))
	Register(workerActionCmd("stop", "stopped", "Stop a worker (it stays stopped after a devctl restart)"))
	Register(workerActionCmd("restart", "restarted", "Restart a worker, for example after a code or .env change"))

	Register(&Cmd{
		Name:        "workers:logs",
		Description: "Show the last lines of a worker log",
		Usage:       "<domain> <type|id> [--lines=N] [--follow]",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain, id, or path (. for the cwd)"},
			{Name: "type|id", Description: "Worker type or worker id"},
		},
		Flags: []FlagDef{
			{Name: "lines", Default: "50", Description: "Number of lines to show"},
			{Name: "follow", Default: "false", Description: "Follow the log output (like tail -f)"},
		},
		Examples: []string{"devctl workers:logs myapp.test queue", "devctl workers:logs myapp.test vite -f", "devctl workers:logs myapp.test queue --lines=200 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("workers:logs", flag.ContinueOnError)
			lines := fs.Int("lines", 50, "")
			follow := fs.Bool("follow", false, "")
			fs.BoolVar(follow, "f", false, "")
			pos, err := parseInterspersed(fs, args)
			if err != nil {
				return err
			}
			if len(pos) < 2 {
				return fmt.Errorf("usage: devctl workers:logs <domain> <type|id> [--lines=N] [--follow]")
			}
			w, err := findWorker(c, pos[0], pos[1])
			if err != nil {
				return err
			}
			if *follow {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer cancel()
				return c.StreamLog(ctx, w.LogID, os.Stdout)
			}
			tail, err := c.GetLogTail(w.LogID, 131072)
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return err
			}
			tail = lastLines(tail, *lines)
			if jsonMode {
				PrintJSON(map[string]any{"id": w.ID, "log_id": w.LogID, "content": tail})
				return nil
			}
			if tail == "" {
				fmt.Println(styleDim.Render("(log is empty)"))
				return nil
			}
			fmt.Println(tail)
			return nil
		},
	})
}

// lastLines returns the last n lines of s without a trailing newline.
func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" || n <= 0 {
		return ""
	}
	parts := strings.Split(s, "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "\n")
}
