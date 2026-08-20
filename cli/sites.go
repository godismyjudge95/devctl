package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			byID := map[string]Site{}
			for _, s := range sites {
				byID[s.ID] = s
			}
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
				worktree := styleDim.Render("—")
				if s.ParentSiteID != nil {
					parent := *s.ParentSiteID
					if p, ok := byID[parent]; ok {
						parent = p.Domain
					}
					branch := ""
					if s.WorktreeBranch != nil {
						branch = " (" + *s.WorktreeBranch + ")"
					}
					worktree = parent + branch
				}
				rows = append(rows, []string{
					s.Domain,
					scheme,
					orDash(s.PHPVersion),
					framework,
					spx,
					worktree,
				})
			}
			Table([]string{"Domain", "Scheme", "PHP", "Framework", "SPX", "Worktree"}, rows)
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
			found, err := findSite(c, args[0], nil)
			if err != nil {
				return err
			}
			sites, err := c.ListSites()
			if err != nil {
				return err
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
			KV("CORS", fmt.Sprintf("%v", found.CORS == 1))
			if found.IsGitRepo == 1 {
				KV("Git remote", found.GitRemoteURL)
			}
			if found.ParentSiteID != nil {
				parent := *found.ParentSiteID
				for i := range sites {
					if sites[i].ID == *found.ParentSiteID {
						parent = sites[i].Domain
						break
					}
				}
				KV("Worktree of", parent)
				if found.WorktreeBranch != nil {
					KV("Branch", *found.WorktreeBranch)
				}
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
				"cors":           found.CORS,
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
		Name:        "sites:cors",
		Description: "Enable or disable Caddy CORS header injection for a site",
		Usage:       "<domain> <enable|disable>",
		Args: []ArgDef{
			{Name: "domain", Description: "Site domain (e.g. myapp.test)"},
			{Name: "action", Description: "enable or disable"},
		},
		Examples: []string{
			"devctl sites:cors myapp.test enable",
			"devctl sites:cors tools.infomedia.test disable",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: devctl sites:cors <domain> <enable|disable>")
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

			want := int64(0)
			if action == "enable" {
				want = 1
			}
			if found.CORS == want {
				state := "disabled"
				if want == 1 {
					state = "enabled"
				}
				fmt.Printf("CORS injection is already %s for %s\n", state, domain)
				return nil
			}

			var aliases []string
			if found.Aliases != "" && found.Aliases != "[]" {
				_ = json.Unmarshal([]byte(found.Aliases), &aliases)
			}
			body := map[string]any{
				"domain":         found.Domain,
				"root_path":      found.RootPath,
				"php_version":    found.PHPVersion,
				"aliases":        aliases,
				"spx_enabled":    found.SPXEnabled,
				"https":          found.HTTPS,
				"cors":           want,
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
			if want == 1 {
				PrintOK("CORS injection enabled for " + domain)
			} else {
				PrintOK("CORS injection disabled for " + domain)
			}
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

	Register(&Cmd{
		Name:        "sites:worktrees",
		Description: "List git worktrees for a site (or all worktrees)",
		Usage:       "[domain]",
		Args:        []ArgDef{{Name: "domain", Description: "Parent site domain, id, or path. Omit to list every worktree.", Optional: true}},
		Examples:    []string{"devctl sites:worktrees", "devctl sites:worktrees myapp.test", "devctl sites:worktrees --json myapp.test"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			sites, err := c.ListSites()
			if err != nil {
				return err
			}
			var rows []Site
			if len(args) == 0 {
				for _, s := range sites {
					if s.ParentSiteID != nil {
						rows = append(rows, s)
					}
				}
			} else {
				parent, err := findSite(c, args[0], sites)
				if err != nil {
					return err
				}
				rows, err = c.ListWorktrees(parent.ID)
				if err != nil {
					return err
				}
			}
			if jsonMode {
				if rows == nil {
					rows = []Site{}
				}
				PrintJSON(rows)
				return nil
			}
			if len(rows) == 0 {
				fmt.Println("No worktrees.")
				return nil
			}
			byID := map[string]Site{}
			for _, s := range sites {
				byID[s.ID] = s
			}
			var table [][]string
			for _, s := range rows {
				parent := ""
				if s.ParentSiteID != nil {
					if p, ok := byID[*s.ParentSiteID]; ok {
						parent = p.Domain
					} else {
						parent = *s.ParentSiteID
					}
				}
				branch := ""
				if s.WorktreeBranch != nil {
					branch = *s.WorktreeBranch
				}
				table = append(table, []string{s.Domain, branch, parent, s.RootPath})
			}
			Table([]string{"Domain", "Branch", "Parent", "Path"}, table)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:worktree:add",
		Description: "Create a git worktree site from a parent (seeds vendor/.env, rewrites URLs)",
		Usage:       "<domain> <branch> [--create] [--copy=...] [--symlink=...] [--no-share]",
		Args: []ArgDef{
			{Name: "domain", Description: "Parent site domain, id, or path (use . for cwd)"},
			{Name: "branch", Description: "Existing branch, or new branch name with --create"},
		},
		Flags: []FlagDef{
			{Name: "create", Default: "false", Description: "Create a new branch from HEAD"},
			{Name: "copy", Description: "Comma-separated paths to copy from parent (default: framework defaults)"},
			{Name: "symlink", Description: "Comma-separated paths to symlink from parent"},
			{Name: "no-share", Default: "false", Description: "Do not copy or symlink anything from the parent"},
		},
		Examples: []string{
			"devctl sites:worktree:add myapp.test feature/auth",
			"devctl sites:worktree:add myapp.test hotfix/now --create",
			"devctl sites:worktree:add --json . feature/x",
			"devctl sites:worktree:add myapp.test feature/x --copy=.env,vendor --symlink=storage/logs",
			"devctl sites:worktree:add myapp.test scratch --create --no-share",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			// Flags may appear before or after positionals. Dispatch stops at the
			// first non-flag, so we scan the whole remainder ourselves.
			var create, noShare bool
			var copyList, symlinkList string
			var pos []string
			for i := 0; i < len(args); i++ {
				a := args[i]
				switch {
				case a == "--create" || a == "--create=true":
					create = true
				case a == "--no-share" || a == "--no-share=true":
					noShare = true
				case strings.HasPrefix(a, "--copy="):
					copyList = strings.TrimPrefix(a, "--copy=")
				case a == "--copy" && i+1 < len(args):
					i++
					copyList = args[i]
				case strings.HasPrefix(a, "--symlink="):
					symlinkList = strings.TrimPrefix(a, "--symlink=")
				case a == "--symlink" && i+1 < len(args):
					i++
					symlinkList = args[i]
				case strings.HasPrefix(a, "-"):
					return fmt.Errorf("unknown flag %s\nusage: devctl sites:worktree:add <domain> <branch> [--create] [--copy=...] [--symlink=...] [--no-share]", a)
				default:
					pos = append(pos, a)
				}
			}
			if len(pos) < 2 {
				return fmt.Errorf("usage: devctl sites:worktree:add <domain> <branch> [--create] [--copy=...] [--symlink=...] [--no-share]")
			}
			ident, branch := pos[0], pos[1]
			parent, err := findSite(c, ident, nil)
			if err != nil {
				return err
			}

			body := map[string]any{
				"branch":        branch,
				"create_branch": create,
				"no_share":      noShare,
			}
			if copyList != "" {
				body["copies"] = splitCSV(copyList)
			}
			if symlinkList != "" {
				body["symlinks"] = splitCSV(symlinkList)
			}

			site, err := c.CreateWorktree(parent.ID, body)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(site)
				return nil
			}
			PrintOK("Worktree created: " + site.Domain)
			KV("Path", site.RootPath)
			if site.WorktreeBranch != nil {
				KV("Branch", *site.WorktreeBranch)
			}
			KV("Parent", parent.Domain)
			fmt.Println()
			fmt.Println(styleDim.Render("vendor/ is copied (not symlinked) so Composer __DIR__ stays in this worktree."))
			fmt.Println(styleDim.Render("If composer.lock differs from the parent, run composer install in the worktree."))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:worktree:rm",
		Description: "Remove a git worktree site (directory, git entry, and Caddy vhost)",
		Usage:       "<worktree-domain>",
		Args:        []ArgDef{{Name: "worktree-domain", Description: "Worktree site domain, id, or path"}},
		Examples:    []string{"devctl sites:worktree:rm myapp-feature-auth.test", "devctl sites:worktree:rm --json myapp-feature-auth.test"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl sites:worktree:rm <worktree-domain>")
			}
			site, err := findSite(c, args[0], nil)
			if err != nil {
				return err
			}
			if site.ParentSiteID == nil {
				return fmt.Errorf("%s is not a worktree (no parent_site_id) — use the parent site's worktree domain", site.Domain)
			}
			if err := c.RemoveWorktree(*site.ParentSiteID, site.ID); err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(map[string]string{"status": "removed", "domain": site.Domain})
				return nil
			}
			PrintOK("Removed worktree " + site.Domain)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:worktree:config",
		Description: "Show or save default symlink/copy paths for future worktrees",
		Usage:       "<domain> [--copy=...] [--symlink=...]",
		Args:        []ArgDef{{Name: "domain", Description: "Parent site domain, id, or path"}},
		Flags: []FlagDef{
			{Name: "copy", Description: "Comma-separated paths to save as copy defaults"},
			{Name: "symlink", Description: "Comma-separated paths to save as symlink defaults"},
		},
		Examples: []string{
			"devctl sites:worktree:config myapp.test",
			"devctl sites:worktree:config myapp.test --copy=.env,vendor,node_modules",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			var copyList, symlinkList string
			var pos []string
			for i := 0; i < len(args); i++ {
				a := args[i]
				switch {
				case strings.HasPrefix(a, "--copy="):
					copyList = strings.TrimPrefix(a, "--copy=")
				case a == "--copy" && i+1 < len(args):
					i++
					copyList = args[i]
				case strings.HasPrefix(a, "--symlink="):
					symlinkList = strings.TrimPrefix(a, "--symlink=")
				case a == "--symlink" && i+1 < len(args):
					i++
					symlinkList = args[i]
				case strings.HasPrefix(a, "-"):
					return fmt.Errorf("unknown flag %s\nusage: devctl sites:worktree:config <domain> [--copy=...] [--symlink=...]", a)
				default:
					pos = append(pos, a)
				}
			}
			if len(pos) == 0 {
				return fmt.Errorf("usage: devctl sites:worktree:config <domain> [--copy=...] [--symlink=...]")
			}
			site, err := findSite(c, pos[0], nil)
			if err != nil {
				return err
			}

			if copyList == "" && symlinkList == "" {
				cfg, err := c.GetWorktreeConfig(site.ID)
				if err != nil {
					return err
				}
				if jsonMode {
					PrintJSON(cfg)
					return nil
				}
				Header("Worktree defaults: " + site.Domain)
				KV("Copies", strings.Join(cfg.Copies, ", "))
				KV("Symlinks", strings.Join(cfg.Symlinks, ", "))
				return nil
			}

			cfg, err := c.GetWorktreeConfig(site.ID)
			if err != nil {
				return err
			}
			if copyList != "" {
				cfg.Copies = splitCSV(copyList)
			}
			if symlinkList != "" {
				cfg.Symlinks = splitCSV(symlinkList)
			}
			saved, err := c.PutWorktreeConfig(site.ID, cfg)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(saved)
				return nil
			}
			PrintOK("Saved worktree defaults for " + site.Domain)
			KV("Copies", strings.Join(saved.Copies, ", "))
			KV("Symlinks", strings.Join(saved.Symlinks, ", "))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "sites:branches",
		Description: "List git branches for a site",
		Usage:       "<domain>",
		Args:        []ArgDef{{Name: "domain", Description: "Site domain, id, or path"}},
		Examples:    []string{"devctl sites:branches myapp.test", "devctl sites:branches --json ."},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl sites:branches <domain>")
			}
			site, err := findSite(c, args[0], nil)
			if err != nil {
				return err
			}
			branches, err := c.ListSiteBranches(site.ID)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(branches)
				return nil
			}
			if len(branches) == 0 {
				fmt.Println("No branches found.")
				return nil
			}
			var rows [][]string
			for _, b := range branches {
				kind := "local"
				if b.IsRemote {
					kind = "remote"
				}
				cur := ""
				if b.IsCurrent {
					cur = "current"
				}
				rows = append(rows, []string{b.Name, kind, cur})
			}
			Table([]string{"Branch", "Kind", ""}, rows)
			return nil
		},
	})
}

func findSite(c *Client, ident string, cached []Site) (*Site, error) {
	sites := cached
	if sites == nil {
		var err error
		sites, err = c.ListSites()
		if err != nil {
			return nil, err
		}
	}
	if ident == "." || strings.HasPrefix(ident, "/") || strings.HasPrefix(ident, "./") || strings.HasPrefix(ident, "../") {
		abs, err := filepath.Abs(ident)
		if err != nil {
			return nil, err
		}
		// Prefer an exact root_path match; fall back to the site whose root is a prefix.
		var prefix *Site
		for i := range sites {
			if sites[i].RootPath == abs {
				return &sites[i], nil
			}
			if prefix == nil && sites[i].RootPath != "" && (abs == sites[i].RootPath || strings.HasPrefix(abs, sites[i].RootPath+string(os.PathSeparator))) {
				s := sites[i]
				prefix = &s
			}
		}
		if prefix != nil {
			return prefix, nil
		}
		return nil, fmt.Errorf("no site found at path %q — run `devctl sites:list`", abs)
	}
	for i := range sites {
		if sites[i].Domain == ident || sites[i].ID == ident {
			return &sites[i], nil
		}
	}
	return nil, fmt.Errorf("no site found with domain %q — run `devctl sites:list`", ident)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return styleDim.Render("—")
	}
	return s
}
