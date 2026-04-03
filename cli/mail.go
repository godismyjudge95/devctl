package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func init() {
	Register(&Cmd{
		Name:        "mail:list",
		Description: "List recent emails captured by Mailpit",
		Usage:       "[--limit=N] [--start=N]",
		Flags: []FlagDef{
			{Name: "limit", Default: "20", Description: "Max emails to return (max 100)"},
			{Name: "start", Default: "0", Description: "Pagination offset"},
		},
		Examples: []string{
			"devctl mail:list",
			"devctl mail:list --limit=50",
			"devctl mail:list --start=20",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fs := flag.NewFlagSet("mail:list", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)
			limit := fs.Int("limit", 20, "max emails")
			start := fs.Int("start", 0, "pagination offset")
			if err := fs.Parse(args); err != nil {
				return err
			}
			if *limit <= 0 || *limit > 100 {
				*limit = 20
			}
			if *start < 0 {
				*start = 0
			}

			resp, err := c.ListMail(*limit, *start)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(resp)
				return nil
			}
			if len(resp.Messages) == 0 {
				fmt.Println("No emails in Mailpit.")
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

	Register(&Cmd{
		Name:        "mail:get",
		Description: "Show the full content of an email",
		Usage:       "<id>",
		Args:        []ArgDef{{Name: "id", Description: "Email ID from mail:list"}},
		Examples:    []string{"devctl mail:get abc123"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl mail:get <id>")
			}
			msg, err := c.GetMail(args[0])
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(msg)
				return nil
			}
			Header("Email: " + msg.Subject)
			KV("From", FormatAddress(msg.From))
			KV("To", FormatAddresses(msg.To))
			if len(msg.Cc) > 0 {
				KV("Cc", FormatAddresses(msg.Cc))
			}
			KV("Date", msg.Created)
			KV("ID", msg.ID)
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
