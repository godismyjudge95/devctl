package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Register(&Cmd{
		Name:        "sites:list",
		Description: "List all managed sites",
		Examples:    []string{"devctl sites:list", "devctl sites:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			sites, err := c.ListSites()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(sites)
				return nil
			}
			if len(sites) == 0 {
				fmt.Println("No sites configured.")
				return nil
			}
			var rows [][]string
			for _, s := range sites {
				scheme := "http"
				if s.HTTPS == 1 {
					scheme = "https"
				}
				spx := styleDim.Render("off")
				if s.SPXEnabled == 1 {
					spx = styleWarn.Render("on")
				}
				framework := s.Framework
				if framework == "" {
					framework = styleDim.Render("—")
				}
				rows = append(rows, []string{
					s.Domain,
					scheme,
					orDash(s.PHPVersion),
					framework,
					spx,
				})
			}
			Table([]string{"Domain", "Scheme", "PHP", "Framework", "SPX"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:get",
		Description: "Show full details for a site",
		Usage:       "<domain>",
		Args:        []ArgDef{{Name: "domain", Description: "Site domain (e.g. myapp.test)"}},
		Examples:    []string{"devctl sites:get myapp.test"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl sites:get <domain>")
			}
			domain := args[0]
			sites, err := c.ListSites()
			if err != nil {
				return err
			}
			var found *Site
			for i := range sites {
				if sites[i].Domain == domain {
					found = &sites[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("no site found with domain %q — run `devctl sites:list` to see all sites", domain)
			}
			if jsonMode {
				PrintJSON(found)
				return nil
			}
			scheme := "http"
			if found.HTTPS == 1 {
				scheme = "https"
			}
			spx := "disabled"
			if found.SPXEnabled == 1 {
				spx = styleWarn.Render("enabled")
			}
			Header("Site: " + found.Domain)
			KV("URL", scheme+"://"+found.Domain)
			KV("ID", found.ID)
			KV("Root path", found.RootPath)
			KV("Public dir", orDash(found.PublicDir))
			KV("PHP version", orDash(found.PHPVersion))
			KV("Framework", orDash(found.Framework))
			KV("SPX profiler", spx)
			KV("HTTPS", fmt.Sprintf("%v", found.HTTPS == 1))
			if found.IsGitRepo == 1 {
				KV("Git remote", found.GitRemoteURL)
			}
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:php",
		Description: "Switch the PHP version for a site",
		Usage:       "<domain> <php-version>",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain (e.g. myapp.test)"},
			{Name: "php-version", Description: "PHP version (e.g. 8.3, 8.4)"},
		},
		Examples: []string{"devctl sites:php myapp.test 8.4", "devctl sites:php myapp.test 8.3"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: devctl sites:php <domain> <php-version>")
			}
			domain, phpVer := args[0], args[1]

			sites, err := c.ListSites()
			if err != nil {
				return err
			}
			var found *Site
			for i := range sites {
				if sites[i].Domain == domain {
					found = &sites[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("no site found with domain %q", domain)
			}

			versions, err := c.ListPHPVersions()
			if err != nil {
				return err
			}
			valid := false
			for _, v := range versions {
				if v.Version == phpVer {
					valid = true
					break
				}
			}
			if !valid {
				available := make([]string, len(versions))
				for i, v := range versions {
					available[i] = v.Version
				}
				return fmt.Errorf("PHP %s is not installed — available: %s", phpVer, strings.Join(available, ", "))
			}

			var aliases []string
			if found.Aliases != "" && found.Aliases != "[]" {
				_ = json.Unmarshal([]byte(found.Aliases), &aliases)
			}
			body := map[string]any{
				"domain":         found.Domain,
				"root_path":      found.RootPath,
				"php_version":    phpVer,
				"aliases":        aliases,
				"spx_enabled":    found.SPXEnabled,
				"https":          found.HTTPS,
				"public_dir":     found.PublicDir,
				"framework":      found.Framework,
				"is_git_repo":    found.IsGitRepo,
				"git_remote_url": found.GitRemoteURL,
			}
			updated, err := c.UpdateSite(found.ID, body)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(updated)
				return nil
			}
			PrintOK(fmt.Sprintf("Switched %s from PHP %s → PHP %s", domain, orDash(found.PHPVersion), phpVer))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:spx",
		Description: "Enable or disable the SPX profiler for a site",
		Usage:       "<domain> <enable|disable>",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain (e.g. myapp.test)"},
			{Name: "action", Description: "enable or disable"},
		},
		Examples: []string{
			"devctl sites:spx myapp.test enable",
			"devctl sites:spx myapp.test disable",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: devctl sites:spx <domain> <enable|disable>")
			}
			domain, action := args[0], args[1]
			if action != "enable" && action != "disable" {
				return fmt.Errorf("action must be 'enable' or 'disable'")
			}

			sites, err := c.ListSites()
			if err != nil {
				return err
			}
			var found *Site
			for i := range sites {
				if sites[i].Domain == domain {
					found = &sites[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("no site found with domain %q", domain)
			}

			if action == "enable" {
				if found.SPXEnabled == 1 {
					fmt.Println("SPX profiler is already enabled for " + domain)
					return nil
				}
				if err := c.EnableSPX(found.ID); err != nil {
					return err
				}
				if jsonMode {
					PrintJSON(map[string]string{"status": "enabled", "domain": domain})
					return nil
				}
				PrintOK("SPX profiler enabled for " + domain)
				fmt.Println()
				fmt.Println("To capture a profile, append to your request URL:")
				fmt.Println(styleDim.Render("  ?SPX_ENABLED=1&SPX_KEY=dev"))
				fmt.Println()
				fmt.Println("View profiles at http://127.0.0.1:4000 → Profiler tab")
			} else {
				if found.SPXEnabled == 0 {
					fmt.Println("SPX profiler is already disabled for " + domain)
					return nil
				}
				if err := c.DisableSPX(found.ID); err != nil {
					return err
				}
				if jsonMode {
					PrintJSON(map[string]string{"status": "disabled", "domain": domain})
					return nil
				}
				PrintOK("SPX profiler disabled for " + domain)
			}
			return nil
		},
	})
}

func orDash(s string) string {
	if s == "" {
		return styleDim.Render("—")
	}
	return s
}
