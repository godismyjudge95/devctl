package cli

import (
	"context"
	"flag"
	"fmt"
	"html"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// mailFilter selects messages. To, From and Subject go to Mailpit search;
// Since is applied to each message's Created time on the client, because
// Mailpit's after: filter only takes a date.
type mailFilter struct {
	To      string
	From    string
	Subject string
	Since   time.Time
}

func (f mailFilter) query() string {
	var parts []string
	for _, t := range []struct{ key, val string }{{"to", f.To}, {"from", f.From}, {"subject", f.Subject}} {
		if v := strings.ReplaceAll(strings.TrimSpace(t.val), `"`, ""); v != "" {
			parts = append(parts, t.key+`:"`+v+`"`)
		}
	}
	return strings.Join(parts, " ")
}

// receivedSince reports whether m arrived at or after f.Since. A message
// with an unparsable Created time is kept, so a format change in Mailpit
// does not hide mail.
func (f mailFilter) receivedSince(m MailMessage) bool {
	if f.Since.IsZero() {
		return true
	}
	created, err := time.Parse(time.RFC3339Nano, m.Created)
	return err != nil || !created.Before(f.Since)
}

var mailFilterFlagDefs = []FlagDef{
	{Name: "to", Description: "Only messages whose To contains this text"},
	{Name: "from", Description: "Only messages whose From contains this text"},
	{Name: "subject", Description: "Only messages whose Subject contains this text"},
	{Name: "since", Description: "Only messages received since a duration ago (15m) or an RFC 3339 time"},
}

// mailFilterFlags registers --to/--from/--subject/--since on fs. Call the
// returned function after fs.Parse to get the filter.
func mailFilterFlags(fs *flag.FlagSet) func(now time.Time) (mailFilter, error) {
	to := fs.String("to", "", "")
	from := fs.String("from", "", "")
	subject := fs.String("subject", "", "")
	since := fs.String("since", "", "")
	return func(now time.Time) (mailFilter, error) {
		f := mailFilter{To: *to, From: *from, Subject: *subject}
		if *since == "" {
			return f, nil
		}
		t, err := parseSince(*since, now)
		f.Since = t
		return f, err
	}
}

func parseSince(s string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q: use a duration (15m, 2h) or an RFC 3339 time", s)
}

// parseMailArgs parses flags that may appear before, between or after the
// positional arguments, and returns the positionals.
func parseMailArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(os.Stderr)
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// findMail returns up to limit messages that match f, newest first.
func findMail(c *Client, f mailFilter, limit, start int) (MailListResponse, error) {
	resp, err := c.SearchMail(f.query(), limit, start)
	if err != nil {
		return resp, err
	}
	kept := resp.Messages[:0]
	for _, m := range resp.Messages {
		if f.receivedSince(m) {
			kept = append(kept, m)
		}
	}
	resp.Messages = kept
	resp.Count = len(kept)
	return resp, nil
}

// resolveMailID turns "latest" into the ID of the newest message matching f.
func resolveMailID(c *Client, id string, f mailFilter) (string, error) {
	if id != "latest" {
		return id, nil
	}
	resp, err := findMail(c, f, 1, 0)
	if err != nil {
		return "", err
	}
	if len(resp.Messages) == 0 {
		return "", fmt.Errorf("no matching email in Mailpit")
	}
	return resp.Messages[0].ID, nil
}

// waitForMail polls until a message matching f arrives at or after cutoff
// and returns the earliest such message. "New" is decided by Mailpit's
// receive time, not by a baseline list of IDs, so a message that lands
// between the start of the command and the first poll is still found.
func waitForMail(ctx context.Context, c *Client, f mailFilter, cutoff time.Time, interval time.Duration) (MailMessage, error) {
	f.Since = cutoff
	for {
		resp, err := findMail(c, f, 50, 0)
		if err != nil {
			return MailMessage{}, err
		}
		if n := len(resp.Messages); n > 0 {
			return resp.Messages[n-1], nil
		}
		select {
		case <-ctx.Done():
			return MailMessage{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

var mailFormats = []string{"text", "html", "raw", "json"}

func printMail(c *Client, id, format string, jsonMode bool) error {
	if jsonMode {
		format = "json"
	}
	if format == "raw" {
		raw, err := c.GetMailRaw(id)
		if err != nil {
			return err
		}
		fmt.Print(raw)
		return nil
	}
	msg, err := c.GetMail(id)
	if err != nil {
		return err
	}
	switch format {
	case "json":
		PrintJSON(msg)
	case "text":
		if msg.Text == "" {
			return fmt.Errorf("email %s has no text part", id)
		}
		fmt.Println(msg.Text)
	case "html":
		if msg.HTML == "" {
			return fmt.Errorf("email %s has no HTML part", id)
		}
		fmt.Println(msg.HTML)
	case "":
		printMailSummary(msg)
	default:
		return fmt.Errorf("invalid --format %q: use one of %s", format, strings.Join(mailFormats, ", "))
	}
	return nil
}

func printMailSummary(msg MailMessageDetail) {
	Header("Email: " + msg.Subject)
	KV("From", FormatAddress(msg.From))
	KV("To", FormatAddresses(msg.To))
	if len(msg.Cc) > 0 {
		KV("Cc", FormatAddresses(msg.Cc))
	}
	KV("Date", msg.Date)
	KV("ID", msg.ID)
	if n := len(msg.Attachments); n > 0 {
		KV("Attachments", fmt.Sprintf("%d (devctl mail:attachments %s)", n, msg.ID))
	}
	fmt.Println()
	if msg.Text != "" {
		fmt.Println(styleDim.Render("── Plain Text ─────────────────────────────"))
		fmt.Println(msg.Text)
	} else if msg.HTML != "" {
		fmt.Println(styleDim.Render("── HTML Body ──────────────────────────────"))
		body := msg.HTML
		if len(body) > 4000 {
			body = body[:4000] + "\n[…truncated]"
		}
		fmt.Println(body)
	}
}

var urlPattern = regexp.MustCompile(`https?://[^\s"'<>` + "`" + `]+`)

// extractLinks returns every http(s) URL in the text and HTML parts, in
// order of first appearance, without duplicates.
func extractLinks(text, htmlBody string) []string {
	seen := map[string]bool{}
	links := []string{}
	for _, body := range []string{text, html.UnescapeString(htmlBody)} {
		for _, u := range urlPattern.FindAllString(body, -1) {
			u = strings.TrimRight(u, ".,;:!?)]}")
			if !seen[u] {
				seen[u] = true
				links = append(links, u)
			}
		}
	}
	return links
}

func init() {
	Register(&Cmd{
		Name:        "mail:list",
		Description: "List recent emails captured by Mailpit",
		Usage:       "[--to=X] [--from=X] [--subject=X] [--since=15m] [--limit=N] [--start=N]",
		Flags: append([]FlagDef{
			{Name: "limit", Default: "20", Description: "Max emails to return (max 100)"},
			{Name: "start", Default: "0", Description: "Pagination offset"},
		}, mailFilterFlagDefs...),
		Examples: []string{
			"devctl mail:list",
			"devctl mail:list --limit=50",
			"devctl mail:list --to=user@example.com --since=10m",
			"devctl mail:list --subject=\"Verify your email\" --json",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:list", flag.ContinueOnError)
			limit := fs.Int("limit", 20, "max emails")
			start := fs.Int("start", 0, "pagination offset")
			filter := mailFilterFlags(fs)
			if _, err := parseMailArgs(fs, args); err != nil {
				return err
			}
			f, err := filter(time.Now())
			if err != nil {
				return err
			}
			if *limit <= 0 || *limit > 100 {
				*limit = 20
			}
			if *start < 0 {
				*start = 0
			}

			resp, err := findMail(c, f, *limit, *start)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(resp)
				return nil
			}
			if len(resp.Messages) == 0 {
				fmt.Println("No matching emails in Mailpit.")
				return nil
			}
			fmt.Printf("%s  total: %d  unread: %s\n\n",
				styleBold.Render(fmt.Sprintf("%d email(s)", len(resp.Messages))),
				resp.Total,
				styleWarn.Render(fmt.Sprintf("%d", resp.Unread)),
			)
			for _, m := range resp.Messages {
				readMark := styleDim.Render("  ")
				if !m.Read {
					readMark = styleWarn.Render("● ")
				}
				to := FormatAddresses(m.To)
				fmt.Printf("%s%s\n", readMark, styleBold.Render(m.Subject))
				fmt.Printf("   %s → %s\n", styleDim.Render(FormatAddress(m.From)), to)
				fmt.Printf("   %s  %s\n\n",
					styleDim.Render(m.Created),
					styleDim.Render(fmt.Sprintf("id: %s", m.ID)),
				)
			}
			fmt.Println(styleDim.Render("Use `devctl mail:get <id>` to read a message."))
			return nil
		},
	})

	formatFlag := FlagDef{Name: "format", Description: "Output: text, html, raw (RFC 822 source) or json. Default is a readable summary"}

	Register(&Cmd{
		Name:        "mail:get",
		Description: "Show the full content of an email",
		Usage:       "<id|latest> [--format=text|html|raw|json]",
		Args:        []ArgDef{{Name: "id", Description: "Email ID from mail:list, or latest"}},
		Flags:       []FlagDef{formatFlag},
		Examples: []string{
			"devctl mail:get abc123",
			"devctl mail:get latest --format=html",
			"devctl mail:get abc123 --format=raw > message.eml",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:get", flag.ContinueOnError)
			format := fs.String("format", "", "output format")
			pos, err := parseMailArgs(fs, args)
			if err != nil {
				return err
			}
			if len(pos) != 1 {
				return fmt.Errorf("usage: devctl mail:get <id|latest> [--format=text|html|raw|json]")
			}
			id, err := resolveMailID(c, pos[0], mailFilter{})
			if err != nil {
				return err
			}
			return printMail(c, id, *format, jsonMode)
		},
	})

	Register(&Cmd{
		Name:        "mail:latest",
		Description: "Show the newest email, optionally the newest that matches filters",
		Usage:       "[--to=X] [--from=X] [--subject=X] [--since=15m] [--format=text|html|raw|json]",
		Flags:       append([]FlagDef{formatFlag}, mailFilterFlagDefs...),
		Examples: []string{
			"devctl mail:latest",
			"devctl mail:latest --to=user@example.com --format=text",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:latest", flag.ContinueOnError)
			format := fs.String("format", "", "output format")
			filter := mailFilterFlags(fs)
			if _, err := parseMailArgs(fs, args); err != nil {
				return err
			}
			f, err := filter(time.Now())
			if err != nil {
				return err
			}
			id, err := resolveMailID(c, "latest", f)
			if err != nil {
				return err
			}
			return printMail(c, id, *format, jsonMode)
		},
	})

	Register(&Cmd{
		Name:        "mail:wait",
		Description: "Wait for a new matching email to arrive, then print it (exit 1 on timeout)",
		Usage:       "[--to=X] [--from=X] [--subject=X] [--since=T] [--timeout=30s] [--format=text|html|raw|json]",
		Flags: append([]FlagDef{
			{Name: "timeout", Default: "30s", Description: "How long to wait before exiting with an error"},
			formatFlag,
		}, mailFilterFlagDefs...),
		Examples: []string{
			"devctl mail:wait --to=user@example.com",
			"devctl mail:wait --subject=\"Reset password\" --timeout=60s --json",
			"devctl mail:wait --to=user@example.com --since=1m",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:wait", flag.ContinueOnError)
			timeout := fs.Duration("timeout", 30*time.Second, "how long to wait")
			format := fs.String("format", "", "output format")
			filter := mailFilterFlags(fs)
			if _, err := parseMailArgs(fs, args); err != nil {
				return err
			}
			// Mailpit stores Created with millisecond precision, so a message
			// received in the same millisecond as now must still count.
			now := time.Now().Truncate(time.Millisecond)
			f, err := filter(now)
			if err != nil {
				return err
			}
			cutoff := now
			if !f.Since.IsZero() {
				cutoff = f.Since
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			ctx, cancelTimeout := context.WithTimeout(ctx, *timeout)
			defer cancelTimeout()

			msg, err := waitForMail(ctx, c, f, cutoff, 500*time.Millisecond)
			if err == context.DeadlineExceeded {
				return fmt.Errorf("no matching email arrived within %s", *timeout)
			}
			if err != nil {
				return err
			}
			return printMail(c, msg.ID, *format, jsonMode)
		},
	})

	Register(&Cmd{
		Name:        "mail:links",
		Description: "Print every URL in an email (text and HTML parts)",
		Usage:       "<id|latest>",
		Args:        []ArgDef{{Name: "id", Description: "Email ID from mail:list, or latest"}},
		Examples:    []string{"devctl mail:links latest", "devctl mail:links abc123 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: devctl mail:links <id|latest>")
			}
			id, err := resolveMailID(c, args[0], mailFilter{})
			if err != nil {
				return err
			}
			msg, err := c.GetMail(id)
			if err != nil {
				return err
			}
			links := extractLinks(msg.Text, msg.HTML)
			if jsonMode {
				PrintJSON(map[string]any{"id": id, "links": links})
				return nil
			}
			for _, l := range links {
				fmt.Println(l)
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "mail:attachments",
		Description: "List the attachments and inline parts of an email",
		Usage:       "<id|latest>",
		Args:        []ArgDef{{Name: "id", Description: "Email ID from mail:list, or latest"}},
		Examples:    []string{"devctl mail:attachments abc123", "devctl mail:attachments latest --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) != 1 {
				return fmt.Errorf("usage: devctl mail:attachments <id|latest>")
			}
			id, err := resolveMailID(c, args[0], mailFilter{})
			if err != nil {
				return err
			}
			msg, err := c.GetMail(id)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"id": id, "attachments": nonNil(msg.Attachments), "inline": nonNil(msg.Inline)})
				return nil
			}
			if len(msg.Attachments)+len(msg.Inline) == 0 {
				fmt.Println("No attachments.")
				return nil
			}
			var rows [][]string
			for _, group := range []struct {
				kind  string
				parts []MailAttachment
			}{{"attachment", msg.Attachments}, {"inline", msg.Inline}} {
				for _, a := range group.parts {
					rows = append(rows, []string{a.PartID, a.FileName, a.ContentType, fmt.Sprintf("%d", a.Size), group.kind})
				}
			}
			Table([]string{"PART", "FILE", "TYPE", "BYTES", "KIND"}, rows)
			fmt.Println(styleDim.Render(fmt.Sprintf("Use `devctl mail:attachment %s <part> --out <file>` to save one.", id)))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "mail:attachment",
		Description: "Save one attachment of an email to a file",
		Usage:       "<id|latest> <part> --out=<file>",
		Args: []ArgDef{
			{Name: "id", Description: "Email ID from mail:list, or latest"},
			{Name: "part", Description: "Part ID from mail:attachments"},
		},
		Flags:    []FlagDef{{Name: "out", Description: "File to write; - writes to stdout"}},
		Examples: []string{"devctl mail:attachment abc123 2 --out=invoice.pdf", "devctl mail:attachment latest 2 --out=- | file -"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:attachment", flag.ContinueOnError)
			out := fs.String("out", "", "output file")
			pos, err := parseMailArgs(fs, args)
			if err != nil {
				return err
			}
			if len(pos) != 2 || *out == "" {
				return fmt.Errorf("usage: devctl mail:attachment <id|latest> <part> --out=<file>")
			}
			id, err := resolveMailID(c, pos[0], mailFilter{})
			if err != nil {
				return err
			}
			data, err := c.GetMailPart(id, pos[1])
			if err != nil {
				return err
			}
			if *out == "-" {
				_, err := os.Stdout.WriteString(data)
				return err
			}
			if err := os.WriteFile(*out, []byte(data), 0o644); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"status": "ok", "id": id, "part": pos[1], "path": *out, "bytes": len(data)})
				return nil
			}
			PrintOK(fmt.Sprintf("Saved %d bytes to %s", len(data), *out))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "mail:delete",
		Description: "Delete one or more emails by ID",
		Usage:       "<id>[,<id>...]",
		Args:        []ArgDef{{Name: "ids", Description: "Comma-separated email IDs to delete"}},
		Examples:    []string{"devctl mail:delete abc123", "devctl mail:delete abc123,def456"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl mail:delete <id>[,<id>...]")
			}
			var ids []string
			for _, a := range args {
				for _, id := range strings.Split(a, ",") {
					id = strings.TrimSpace(id)
					if id != "" {
						ids = append(ids, id)
					}
				}
			}
			if len(ids) == 0 {
				return fmt.Errorf("no valid IDs provided")
			}
			if err := c.DeleteMail(ids); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]any{"status": "ok", "deleted": len(ids)})
				return nil
			}
			PrintOK(fmt.Sprintf("Deleted %d email(s)", len(ids)))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "mail:clear",
		Description: "Delete all emails from Mailpit",
		Examples:    []string{"devctl mail:clear"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if err := c.DeleteAllMail(); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "ok"})
				return nil
			}
			PrintOK("All emails deleted from Mailpit")
			return nil
		},
	})
}

func nonNil(a []MailAttachment) []MailAttachment {
	if a == nil {
		return []MailAttachment{}
	}
	return a
}
