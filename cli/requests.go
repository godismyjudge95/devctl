package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/gorilla/websocket"

	"github.com/danielgormly/devctl/inspector"
)

// requestFilter is the set of list filters shared by the requests:* commands.
type requestFilter struct {
	site     string
	kind     string
	problems bool
	limit    int
}

func (f *requestFilter) flags(fs *flag.FlagSet, limit int) {
	fs.StringVar(&f.site, "site", "", "only requests to this host")
	fs.StringVar(&f.kind, "kind", "", "http, command, or job")
	fs.BoolVar(&f.problems, "problems", false, "only units with problems")
	fs.IntVar(&f.limit, "limit", limit, "max units")
}

func (f requestFilter) query() string {
	v := url.Values{}
	if f.site != "" {
		v.Set("site", f.site)
	}
	if f.kind != "" {
		v.Set("kind", f.kind)
	}
	if f.problems {
		v.Set("problems", "true")
	}
	if f.limit > 0 {
		v.Set("limit", strconv.Itoa(f.limit))
	}
	return v.Encode()
}

func (f requestFilter) matches(s inspector.Summary) bool {
	return (f.site == "" || s.Meta.Host == f.site) &&
		(f.kind == "" || s.Meta.Kind == f.kind) &&
		(!f.problems || len(s.Problems) > 0)
}

var requestFilterFlags = []FlagDef{
	{Name: "site", Description: "Only requests to this host (e.g. myapp.test)"},
	{Name: "kind", Description: "Only this unit kind: http, command, or job"},
	{Name: "problems", Default: "false", Description: "Only units with N+1, duplicate, or slow problems"},
}

func init() {
	Register(&Cmd{
		Name:        "requests:list",
		Description: "List recent PHP requests, jobs, and commands from the request inspector",
		Usage:       "[--site=<host>] [--kind=http|command|job] [--problems] [--limit=20]",
		Flags:       append(append([]FlagDef{}, requestFilterFlags...), FlagDef{Name: "limit", Default: "20", Description: "Max units"}),
		Examples:    []string{"devctl requests:list", "devctl requests:list --site=myapp.test --problems --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			var f requestFilter
			fs := flag.NewFlagSet("requests:list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			f.flags(fs, 20)
			if err := fs.Parse(args); err != nil {
				return err
			}
			units, err := c.ListRequests(f)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(units)
				return nil
			}
			if len(units) == 0 {
				fmt.Println("No requests recorded. Load a page, or run php artisan, with the inspector on.")
				return nil
			}
			rows := make([][]string, 0, len(units))
			for _, u := range units {
				rows = append(rows, []string{u.ID, u.Meta.Kind, describeUnit(u.Meta), statusOf(u), durationOf(u), strconv.Itoa(u.Counts["query"]), problemTags(u.Problems)})
			}
			Table([]string{"ID", "KIND", "WHAT", "STATUS", "TIME", "SQL", "PROBLEMS"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "requests:show",
		Description: "Show one request with its queries, dumps, logs, exceptions, jobs, cache, HTTP calls, views, and problems",
		Usage:       "<id>",
		Args:        []ArgDef{{Name: "id", Description: "Unit id from requests:list or the X-Devctl-Request-Id response header"}},
		Examples:    []string{"devctl requests:show 9f2c4e1a7b3d5c60", "devctl requests:show 9f2c4e1a7b3d5c60 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl requests:show <id>")
			}
			d, err := c.GetRequest(args[0])
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(d)
				return nil
			}
			printRequest(d)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "requests:problems",
		Description: "List N+1, duplicate, and slow query or request problems across recent requests",
		Usage:       "[--site=<host>] [--kind=http|command|job] [--limit=50]",
		Flags:       append(append([]FlagDef{}, requestFilterFlags[:2]...), FlagDef{Name: "limit", Default: "50", Description: "Max units to scan"}),
		Examples:    []string{"devctl requests:problems", "devctl requests:problems --site=myapp.test --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			var f requestFilter
			fs := flag.NewFlagSet("requests:problems", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			f.flags(fs, 50)
			if err := fs.Parse(args); err != nil {
				return err
			}
			f.problems = true
			units, err := c.ListRequests(f)
			if err != nil {
				return err
			}
			found := flattenProblems(units)
			if jsonMode {
				PrintJSON(found)
				return nil
			}
			if len(found) == 0 {
				PrintOK("No problems in the recorded requests")
				return nil
			}
			rows := make([][]string, 0, len(found))
			for _, p := range found {
				rows = append(rows, []string{p.ID, string(p.Kind), countOf(p.Problem), fmt.Sprintf("%.1f ms", p.Ms), callerOf(p.Caller), clip(p.SQL, 80), p.What})
			}
			Table([]string{"ID", "PROBLEM", "COUNT", "TIME", "CALLER", "SQL", "REQUEST"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "requests:watch",
		MCP:         MCPHidden,
		Description: "Print each request as it finishes. With --json, one JSON object per line (NDJSON)",
		Usage:       "[--site=<host>] [--kind=http|command|job] [--problems]",
		Flags:       requestFilterFlags,
		Examples:    []string{"devctl requests:watch", "devctl requests:watch --problems --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			var f requestFilter
			fs := flag.NewFlagSet("requests:watch", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			f.flags(fs, 0)
			if err := fs.Parse(args); err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return c.WatchRequests(ctx, f, os.Stdout, jsonMode)
		},
	})
}

// ListRequests calls GET /api/requests.
func (c *Client) ListRequests(f requestFilter) ([]inspector.Summary, error) {
	var out []inspector.Summary
	path := "/api/requests"
	if q := f.query(); q != "" {
		path += "?" + q
	}
	return out, c.get(path, &out)
}

// GetRequest calls GET /api/requests/{id}.
func (c *Client) GetRequest(id string) (inspector.Detail, error) {
	var out inspector.Detail
	return out, c.get("/api/requests/"+url.PathEscape(id), &out)
}

// WatchRequests follows /ws/requests and writes each finished unit that
// matches f: one JSON line in jsonMode, else one summary line.
func (c *Client) WatchRequests(ctx context.Context, f requestFilter, w io.Writer, jsonMode bool) error {
	wsURL := "ws" + strings.TrimPrefix(c.base, "http") + "/ws/requests"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect %s: %w", wsURL, err)
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	if !jsonMode {
		fmt.Fprintln(os.Stderr, styleDim.Render("Watching requests. Ctrl-C to stop."))
	}
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var u inspector.Summary
		if err := json.Unmarshal(msg, &u); err != nil || !u.Final || !f.matches(u) {
			continue
		}
		if jsonMode {
			line, _ := json.Marshal(u)
			fmt.Fprintf(w, "%s\n", line)
			continue
		}
		fmt.Fprintf(w, "%s  %-7s  %-40s  %s  %s  %s\n", u.ID, u.Meta.Kind, clip(describeUnit(u.Meta), 40), statusOf(u), durationOf(u), problemTags(u.Problems))
	}
}

// foundProblem is one problem with the unit it was found in.
type foundProblem struct {
	ID   string `json:"id"`
	What string `json:"what"`
	inspector.Problem
}

func flattenProblems(units []inspector.Summary) []foundProblem {
	out := []foundProblem{}
	for _, u := range units {
		for _, p := range u.Problems {
			out = append(out, foundProblem{ID: u.ID, What: describeUnit(u.Meta), Problem: p})
		}
	}
	return out
}

func describeUnit(m inspector.Meta) string {
	switch m.Kind {
	case "http":
		return strings.TrimSpace(m.Method + " " + m.URL)
	case "job":
		return m.Job
	default:
		if m.Artisan != "" {
			return "artisan " + m.Artisan
		}
		return m.Command
	}
}

func statusOf(u inspector.Summary) string {
	switch {
	case !u.Final:
		return "running"
	case u.Meta.Status != 0:
		return strconv.Itoa(u.Meta.Status)
	case u.Meta.JobStatus != "":
		return u.Meta.JobStatus
	}
	return "done"
}

func durationOf(u inspector.Summary) string {
	if !u.Final {
		return "-"
	}
	return fmt.Sprintf("%.0f ms", u.Meta.DurationMs)
}

var problemLabels = map[inspector.ProblemKind]string{
	inspector.NPlusOne:    "N+1",
	inspector.Duplicate:   "duplicate",
	inspector.SlowQuery:   "slow query",
	inspector.SlowRequest: "slow request",
}

func problemTags(ps []inspector.Problem) string {
	counts := map[inspector.ProblemKind]int{}
	var order []inspector.ProblemKind
	for _, p := range ps {
		if counts[p.Kind] == 0 {
			order = append(order, p.Kind)
		}
		counts[p.Kind]++
	}
	tags := make([]string, 0, len(order))
	for _, k := range order {
		tags = append(tags, fmt.Sprintf("%s x%d", problemLabels[k], counts[k]))
	}
	return strings.Join(tags, ", ")
}

func countOf(p inspector.Problem) string {
	if p.Count == 0 {
		return ""
	}
	return strconv.Itoa(p.Count)
}

func callerOf(c *inspector.Caller) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.File, c.Line)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// shownEvent holds the event fields requests:show prints.
type shownEvent struct {
	Kind    string            `json:"kind"`
	T       float64           `json:"t"`
	Caller  *inspector.Caller `json:"caller"`
	SQL     string            `json:"sql"`
	Ms      *float64          `json:"ms"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Class   string            `json:"class"`
	File    string            `json:"file"`
	Line    int               `json:"line"`
	Phase   string            `json:"phase"`
	Name    string            `json:"name"`
	Op      string            `json:"op"`
	Key     string            `json:"key"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Status  int               `json:"status"`
	Error   string            `json:"error"`
}

func (e shownEvent) text() string {
	switch e.Kind {
	case "query":
		ms := ""
		if e.Ms != nil {
			ms = fmt.Sprintf("%.2f ms  ", *e.Ms)
		}
		return ms + e.SQL
	case "log":
		return e.Level + ": " + e.Message
	case "exception":
		return fmt.Sprintf("%s: %s (%s:%d)", e.Class, e.Message, e.File, e.Line)
	case "job":
		return e.Phase + " " + e.Name
	case "cache":
		return e.Op + " " + e.Key
	case "http":
		if e.Error != "" {
			return fmt.Sprintf("%s %s  error: %s", e.Method, e.URL, e.Error)
		}
		return fmt.Sprintf("%s %s  %d", e.Method, e.URL, e.Status)
	case "view":
		return e.Name
	}
	return ""
}

func printRequest(d inspector.Detail) {
	Header(describeUnit(d.Meta))
	KV("ID", d.ID)
	KV("Kind", d.Meta.Kind)
	KV("Status", statusOf(d.Summary))
	KV("Duration", durationOf(d.Summary))
	if d.Meta.Route != "" {
		KV("Route", d.Meta.Route)
	}
	if d.Meta.Parent != "" {
		KV("Parent", d.Meta.Parent)
	}
	KV("Queries", fmt.Sprintf("%d (%.1f ms)", d.Counts["query"], d.QueryMs))
	if len(d.Dumps) > 0 {
		KV("Dumps", strconv.Itoa(len(d.Dumps)))
	}
	if d.Meta.Dropped > 0 {
		KV("Dropped", fmt.Sprintf("%d events over the limit", d.Meta.Dropped))
	}

	if len(d.Problems) > 0 {
		fmt.Println()
		Header("Problems")
		for _, p := range d.Problems {
			line := problemLabels[p.Kind]
			if p.Count > 0 {
				line += fmt.Sprintf(" x%d", p.Count)
			}
			line += fmt.Sprintf("  %.1f ms", p.Ms)
			if p.Caller != nil {
				line += "  " + callerOf(p.Caller)
			}
			fmt.Println("  " + styleWarn.Render(line))
			if p.SQL != "" {
				fmt.Println("    " + styleDim.Render(clip(p.SQL, 200)))
			}
		}
	}

	if len(d.Events) > 0 {
		fmt.Println()
		Header("Events")
	}
	for _, raw := range d.Events {
		var e shownEvent
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		fmt.Printf("  %s %-9s %s\n", styleDim.Render(fmt.Sprintf("%8.1f ms", e.T)), e.Kind, clip(e.text(), 200))
		if e.Caller != nil {
			fmt.Printf("  %s\n", styleDim.Render(strings.Repeat(" ", 22)+callerOf(e.Caller)))
		}
	}
}
