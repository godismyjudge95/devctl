package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTools(s *server.MCPServer, c *client) {
	registerListSitesTool(s, c)
	registerGetSiteDetailTool(s, c)
	registerSwitchPHPVersionTool(s, c)
	registerToggleSPXTool(s, c)
	registerListServicesTool(s, c)
	registerRestartServiceTool(s, c)
	registerStartServiceTool(s, c)
	registerStopServiceTool(s, c)
	registerGetServiceCredentialsTool(s, c)
	registerListPHPVersionsTool(s, c)
	registerGetSPXProfilesTool(s, c)
	registerGetSPXProfileDetailTool(s, c)
	registerGetDumpsTool(s, c)
	registerClearDumpsTool(s, c)
	registerListLogsTool(s, c)
	registerGetLogTailTool(s, c)
	registerClearLogTool(s, c)
	registerGetPHPSettingsTool(s, c)
	registerSetPHPSettingsTool(s, c)
	registerGetSettingsTool(s, c)
	registerSetSettingsTool(s, c)
	registerCheckDNSSetupTool(s, c)
	registerConfigureDNSTool(s, c)
	registerTeardownDNSTool(s, c)
	registerTrustCATool(s, c)
	registerListMailTool(s, c)
	registerGetMailTool(s, c)
	registerDeleteMailTool(s, c)
	registerDeleteAllMailTool(s, c)
}

// listSites — returns a summary of all sites.
func registerListSitesTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("listSites",
		mcp.WithDescription("List all sites managed by devctl — domain, root path, PHP version, framework, HTTPS, and SPX profiler state."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sites, err := c.listSites()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(sites) == 0 {
			return mcp.NewToolResultText("No sites configured in devctl."), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("devctl manages %d site(s):\n\n", len(sites)))
		for _, st := range sites {
			spx := "disabled"
			if st.SPXEnabled == 1 {
				spx = "ENABLED"
			}
			scheme := "http"
			if st.HTTPS == 1 {
				scheme = "https"
			}
			git := ""
			if st.IsGitRepo == 1 {
				git = fmt.Sprintf(", git: %s", st.GitRemoteURL)
			}
			sb.WriteString(fmt.Sprintf("• %s (%s://%s)\n", st.Domain, scheme, st.Domain))
			sb.WriteString(fmt.Sprintf("  id: %s | php: %s | framework: %s | spx: %s%s\n", st.ID, orUnset(st.PHPVersion), orUnset(st.Framework), spx, git))
			sb.WriteString(fmt.Sprintf("  root: %s\n\n", st.RootPath))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getSiteDetail — returns full detail for a site by domain.
func registerGetSiteDetailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getSiteDetail",
		mcp.WithDescription("Get full details for a single devctl site by domain name, including PHP version, SPX profiler state, HTTPS config, public directory, and git info."),
		mcp.WithString("domain",
			mcp.Required(),
			mcp.Description("The site domain (e.g. myapp.test)"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		domain, err := req.RequireString("domain")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		sites, err := c.listSites()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, st := range sites {
			if st.Domain == domain {
				b, _ := json.MarshalIndent(st, "", "  ")
				return mcp.NewToolResultText(string(b)), nil
			}
		}
		return mcp.NewToolResultError(fmt.Sprintf("No site found with domain %q. Use listSites to see all available domains.", domain)), nil
	})
}

// switchPHPVersion — updates the PHP version for a site and restarts FPM.
func registerSwitchPHPVersionTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("switchPHPVersion",
		mcp.WithDescription("Switch the PHP version for a site. This updates the site config and Caddy vhost, then starts the appropriate PHP-FPM process. Always call listSites and listPHPVersions first to confirm valid domain and version."),
		mcp.WithString("domain",
			mcp.Required(),
			mcp.Description("The site domain (e.g. myapp.test)"),
		),
		mcp.WithString("php_version",
			mcp.Required(),
			mcp.Description("The PHP version string (e.g. '8.3', '8.4'). Must be an installed version — use listPHPVersions to check."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		domain, err := req.RequireString("domain")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		phpVer, err := req.RequireString("php_version")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Find the site.
		sites, err := c.listSites()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var found *site
		for i := range sites {
			if sites[i].Domain == domain {
				found = &sites[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("No site found with domain %q.", domain)), nil
		}

		// Validate PHP version is installed.
		versions, err := c.listPHPVersions()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		validVersion := false
		for _, v := range versions {
			if v.Version == phpVer {
				validVersion = true
				break
			}
		}
		if !validVersion {
			available := make([]string, len(versions))
			for i, v := range versions {
				available[i] = v.Version
			}
			return mcp.NewToolResultError(fmt.Sprintf("PHP version %q is not installed. Available: %s", phpVer, strings.Join(available, ", "))), nil
		}

		// Reconstruct the full update body, preserving all existing fields.
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
		updated, err := c.updateSite(found.ID, body)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update site: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"Switched %s from PHP %s to PHP %s.\ndevctl has updated the Caddy vhost and started php-fpm-%s.\nSite ID: %s",
			domain, orUnset(found.PHPVersion), updated.PHPVersion, phpVer, found.ID,
		)), nil
	})
}

// toggleSPX — enables or disables the SPX profiler for a site.
func registerToggleSPXTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("toggleSPXProfiler",
		mcp.WithDescription("Enable or disable the SPX PHP profiler for a site. When enabled, add ?SPX_ENABLED=1&SPX_KEY=dev to any request URL (or set a cookie) to capture a profile. View results in the devctl UI at http://127.0.0.1:4000 → Profiler tab."),
		mcp.WithString("domain",
			mcp.Required(),
			mcp.Description("The site domain (e.g. myapp.test)"),
		),
		mcp.WithString("action",
			mcp.Required(),
			mcp.Description("'enable' or 'disable'"),
			mcp.Enum("enable", "disable"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		domain, err := req.RequireString("domain")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		action, err := req.RequireString("action")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		sites, err := c.listSites()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var found *site
		for i := range sites {
			if sites[i].Domain == domain {
				found = &sites[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("No site found with domain %q.", domain)), nil
		}

		switch action {
		case "enable":
			if found.SPXEnabled == 1 {
				return mcp.NewToolResultText(fmt.Sprintf("SPX profiler is already enabled for %s.", domain)), nil
			}
			if err := c.enableSPX(found.ID); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to enable SPX: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf(
				"SPX profiler enabled for %s.\n\nTo capture a profile:\n1. Append ?SPX_ENABLED=1&SPX_KEY=dev to your request URL\n   e.g. https://%s/your-endpoint?SPX_ENABLED=1&SPX_KEY=dev\n2. Or set cookie: SPX_ENABLED=1; SPX_KEY=dev\n\nView captured profiles in the devctl UI → Profiler tab:\nhttp://127.0.0.1:4000",
				domain, domain,
			)), nil
		case "disable":
			if found.SPXEnabled == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("SPX profiler is already disabled for %s.", domain)), nil
			}
			if err := c.disableSPX(found.ID); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to disable SPX: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("SPX profiler disabled for %s.", domain)), nil
		default:
			return mcp.NewToolResultError("action must be 'enable' or 'disable'"), nil
		}
	})
}

// listServices — returns all service statuses.
func registerListServicesTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("listServices",
		mcp.WithDescription("List all devctl-managed services and their current status (running/stopped/pending/warning). Includes Caddy, PHP-FPM instances, Redis, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Reverb, DNS, and more."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		svcs, err := c.listServices()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(svcs) == 0 {
			return mcp.NewToolResultText("No services found."), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("devctl service status (%d services):\n\n", len(svcs)))

		running, stopped, other := 0, 0, 0
		for _, svc := range svcs {
			if !svc.Installed {
				continue
			}
			icon := statusIcon(svc.Status)
			ver := ""
			if svc.Version != "" {
				ver = " v" + svc.Version
			}
			update := ""
			if svc.UpdateAvailable {
				update = fmt.Sprintf(" [update available: %s]", svc.LatestVersion)
			}
			sb.WriteString(fmt.Sprintf("%s %s (%s)%s%s\n", icon, svc.Label, svc.ID, ver, update))
			switch svc.Status {
			case "running":
				running++
			case "stopped":
				stopped++
			default:
				other++
			}
		}
		sb.WriteString(fmt.Sprintf("\nSummary: %d running, %d stopped, %d other", running, stopped, other))
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// restartService — restarts a named service.
func registerRestartServiceTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("restartService",
		mcp.WithDescription("Restart a devctl-managed service by its ID. Common IDs: caddy, redis, postgres, mysql, meilisearch, typesense, mailpit, reverb, dns. PHP-FPM services use the pattern php-fpm-{version} e.g. php-fpm-8.3."),
		mcp.WithString("service_id",
			mcp.Required(),
			mcp.Description("The service ID (e.g. 'caddy', 'redis', 'php-fpm-8.3'). Use listServices to get valid IDs."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		serviceID, err := req.RequireString("service_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		svcs, err := c.listServices()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var found *serviceState
		for i := range svcs {
			if svcs[i].ID == serviceID {
				found = &svcs[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q not found. Use listServices to see available service IDs.", serviceID)), nil
		}
		if !found.Installed {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q (%s) is not installed.", serviceID, found.Label)), nil
		}

		if err := c.restartService(serviceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to restart %s: %v", serviceID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Restart command sent to %s (%s).", found.Label, serviceID)), nil
	})
}

// listPHPVersions — lists installed PHP versions.
func registerListPHPVersionsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("listPHPVersions",
		mcp.WithDescription("List all PHP versions known to devctl, their FPM socket path, and current running status."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		versions, err := c.listPHPVersions()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(versions) == 0 {
			return mcp.NewToolResultText("No PHP versions installed."), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d PHP version(s):\n\n", len(versions)))
		for _, v := range versions {
			sb.WriteString(fmt.Sprintf("• PHP %s — status: %s, socket: %s\n", v.Version, v.Status, v.FPMSocket))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getSPXProfiles — lists recent SPX profiler captures.
func registerGetSPXProfilesTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getSPXProfiles",
		mcp.WithDescription("List recent SPX profiler captures. Optionally filter by site domain. Returns key (use in devctl UI), PHP version, endpoint, wall time, and peak memory."),
		mcp.WithString("domain",
			mcp.Description("Filter profiles by site domain (optional, e.g. myapp.test)"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		domain := req.GetString("domain", "")
		profiles, err := c.listSPXProfiles(domain)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(profiles) == 0 {
			msg := "No SPX profiles found."
			if domain != "" {
				msg = fmt.Sprintf("No SPX profiles found for domain %q.", domain)
			}
			return mcp.NewToolResultText(msg), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d SPX profile(s)%s:\n\n", len(profiles), filterSuffix(domain)))
		for _, p := range profiles {
			memMB := float64(p.PeakMemoryBytes) / 1024 / 1024
			sb.WriteString(fmt.Sprintf("• %s %s → %s (PHP %s)\n", p.Method, p.URI, p.Domain, p.PHPVersion))
			sb.WriteString(fmt.Sprintf("  wall time: %.1fms | peak mem: %.1fMB | functions called: %d\n", p.WallTimeMs, memMB, p.CalledFuncCount))
			sb.WriteString(fmt.Sprintf("  key: %s  (view flamegraph at http://127.0.0.1:4000 → Profiler)\n\n", p.Key))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getDumps — lists recent php_dd() dumps.
func registerGetDumpsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getDumps",
		mcp.WithDescription("List recent php_dd() variable dumps captured by devctl. Optionally filter by site domain."),
		mcp.WithString("domain",
			mcp.Description("Filter dumps by site domain (optional, e.g. myapp.test)"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		domain := req.GetString("domain", "")
		resp, err := c.listDumps(domain)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(resp.Dumps) == 0 {
			msg := "No dumps found."
			if domain != "" {
				msg = fmt.Sprintf("No dumps found for domain %q.", domain)
			}
			return mcp.NewToolResultText(msg), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d dump(s) (total: %d)%s:\n\n", len(resp.Dumps), resp.Total, filterSuffix(domain)))
		for _, d := range resp.Dumps {
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
			sb.WriteString(fmt.Sprintf("• Dump #%d — %s:%d (site: %s)\n", d.ID, file, line, site))
			nodes := d.Nodes
			if len(nodes) > 300 {
				nodes = nodes[:300] + "..."
			}
			sb.WriteString(fmt.Sprintf("  data: %s\n\n", nodes))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// listLogs — lists available log files.
func registerListLogsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("listLogs",
		mcp.WithDescription("List all log files available in devctl (e.g. caddy, php-fpm-8.3, mailpit). Use the returned IDs with getLogTail to read log content."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logs, err := c.listLogFiles()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(logs) == 0 {
			return mcp.NewToolResultText("No log files found."), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d log file(s):\n\n", len(logs)))
		for _, l := range logs {
			kb := float64(l.Size) / 1024
			sb.WriteString(fmt.Sprintf("• %s (%s, %.1f KB)\n", l.ID, l.Name, kb))
		}
		sb.WriteString("\nUse getLogTail(log_id) to read the tail of any log.")
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getLogTail — reads the tail of a log file.
func registerGetLogTailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getLogTail",
		mcp.WithDescription("Read the last portion of a devctl log file. Use listLogs to get valid log IDs (e.g. 'caddy', 'php-fpm-8.3', 'mailpit'). Returns plain text log lines."),
		mcp.WithString("log_id",
			mcp.Required(),
			mcp.Description("The log file ID (e.g. 'caddy', 'php-fpm-8.3'). Use listLogs to see available IDs."),
		),
		mcp.WithNumber("bytes",
			mcp.Description("How many bytes to read from the end of the file (default: 16384, max: 131072)."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logID, err := req.RequireString("log_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		bytes := req.GetInt("bytes", 16384)
		if bytes <= 0 {
			bytes = 16384
		}
		if bytes > 131072 {
			bytes = 131072
		}

		tail, err := c.getLogTail(logID, bytes)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read log %q: %v", logID, err)), nil
		}
		if strings.TrimSpace(tail) == "" {
			return mcp.NewToolResultText(fmt.Sprintf("Log %q is empty.", logID)), nil
		}
		return mcp.NewToolResultText(tail), nil
	})
}

// startService — starts a stopped service.
func registerStartServiceTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("startService",
		mcp.WithDescription("Start a stopped devctl-managed service by its ID. Common IDs: caddy, redis, postgres, mysql, meilisearch, typesense, mailpit, reverb, dns. PHP-FPM services use the pattern php-fpm-{version} e.g. php-fpm-8.3."),
		mcp.WithString("service_id",
			mcp.Required(),
			mcp.Description("The service ID (e.g. 'caddy', 'redis', 'php-fpm-8.3'). Use listServices to get valid IDs."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		serviceID, err := req.RequireString("service_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		svcs, err := c.listServices()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var found *serviceState
		for i := range svcs {
			if svcs[i].ID == serviceID {
				found = &svcs[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q not found. Use listServices to see available service IDs.", serviceID)), nil
		}
		if !found.Installed {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q (%s) is not installed.", serviceID, found.Label)), nil
		}
		if err := c.startService(serviceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to start %s: %v", serviceID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Start command sent to %s (%s).", found.Label, serviceID)), nil
	})
}

// stopService — stops a running service.
func registerStopServiceTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("stopService",
		mcp.WithDescription("Stop a running devctl-managed service by its ID. Note: required services (e.g. caddy, dns) cannot be stopped."),
		mcp.WithString("service_id",
			mcp.Required(),
			mcp.Description("The service ID (e.g. 'redis', 'mailpit'). Use listServices to get valid IDs."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		serviceID, err := req.RequireString("service_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		svcs, err := c.listServices()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var found *serviceState
		for i := range svcs {
			if svcs[i].ID == serviceID {
				found = &svcs[i]
				break
			}
		}
		if found == nil {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q not found. Use listServices to see available service IDs.", serviceID)), nil
		}
		if found.Required {
			return mcp.NewToolResultError(fmt.Sprintf("Service %q (%s) is required and cannot be stopped.", serviceID, found.Label)), nil
		}
		if err := c.stopService(serviceID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to stop %s: %v", serviceID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Stop command sent to %s (%s).", found.Label, serviceID)), nil
	})
}

// getServiceCredentials — returns credentials for a service.
func registerGetServiceCredentialsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getServiceCredentials",
		mcp.WithDescription("Get connection credentials for a devctl-managed service (e.g. postgres, mysql). Returns host, port, username, password, and database name where applicable."),
		mcp.WithString("service_id",
			mcp.Required(),
			mcp.Description("The service ID (e.g. 'postgres', 'mysql'). Use listServices to see services with has_credentials: true."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		serviceID, err := req.RequireString("service_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		creds, err := c.getServiceCredentials(serviceID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get credentials for %q: %v", serviceID, err)), nil
		}
		if len(creds) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No credentials found for service %q.", serviceID)), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Credentials for %s:\n\n", serviceID))
		for k, v := range creds {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getSPXProfileDetail — returns top hotspot functions for a profile.
func registerGetSPXProfileDetailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getSPXProfileDetail",
		mcp.WithDescription("Get the top CPU hotspot functions for a specific SPX profiler capture. Use getSPXProfiles first to find the profile key. Returns up to 15 functions sorted by exclusive CPU time."),
		mcp.WithString("key",
			mcp.Required(),
			mcp.Description("The profile key from getSPXProfiles."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		detail, err := c.getSPXProfileDetail(key)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to load profile %q: %v", key, err)), nil
		}
		memMB := float64(detail.PeakMemoryBytes) / 1024 / 1024
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Profile: %s %s → %s (PHP %s)\n", detail.Method, detail.URI, detail.Domain, detail.PHPVersion))
		sb.WriteString(fmt.Sprintf("Wall time: %.1fms | Peak memory: %.1fMB | Functions called: %d\n\n", detail.WallTimeMs, memMB, detail.CalledFuncCount))
		if len(detail.Functions) == 0 {
			sb.WriteString("No function data available.\n")
		} else {
			limit := len(detail.Functions)
			if limit > 15 {
				limit = 15
			}
			sb.WriteString(fmt.Sprintf("Top %d functions by exclusive CPU time:\n\n", limit))
			sb.WriteString(fmt.Sprintf("%-60s %8s %8s %8s\n", "Function", "Calls", "Excl ms", "Excl %"))
			sb.WriteString(strings.Repeat("-", 90) + "\n")
			for _, f := range detail.Functions[:limit] {
				name := f.Name
				if len(name) > 58 {
					name = "…" + name[len(name)-57:]
				}
				sb.WriteString(fmt.Sprintf("%-60s %8d %8.2f %7.1f%%\n", name, f.Calls, f.ExclusiveMs, f.ExclusivePct))
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// clearDumps — deletes all php_dd() dumps.
func registerClearDumpsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("clearDumps",
		mcp.WithDescription("Delete all captured php_dd() variable dumps from devctl."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := c.clearDumps(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to clear dumps: %v", err)), nil
		}
		return mcp.NewToolResultText("All php_dd() dumps have been cleared."), nil
	})
}

// clearLog — truncates a log file.
func registerClearLogTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("clearLog",
		mcp.WithDescription("Clear (truncate) a devctl log file by its ID. Use listLogs to get valid log IDs."),
		mcp.WithString("log_id",
			mcp.Required(),
			mcp.Description("The log file ID to clear (e.g. 'caddy', 'php-fpm-8.3'). Use listLogs to see available IDs."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		logID, err := req.RequireString("log_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := c.clearLog(logID); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to clear log %q: %v", logID, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Log %q has been cleared.", logID)), nil
	})
}

// getPHPSettings — reads current PHP ini settings.
func registerGetPHPSettingsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getPHPSettings",
		mcp.WithDescription("Get the current PHP ini settings managed by devctl: memory_limit, upload_max_filesize, max_execution_time, post_max_size."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		settings, err := c.getPHPSettings()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get PHP settings: %v", err)), nil
		}
		var sb strings.Builder
		sb.WriteString("Current PHP settings (applies to all installed PHP versions):\n\n")
		sb.WriteString(fmt.Sprintf("  memory_limit:        %s\n", settings.MemoryLimit))
		sb.WriteString(fmt.Sprintf("  upload_max_filesize: %s\n", settings.UploadMaxFilesize))
		sb.WriteString(fmt.Sprintf("  post_max_size:       %s\n", settings.PostMaxSize))
		sb.WriteString(fmt.Sprintf("  max_execution_time:  %s\n", settings.MaxExecutionTime))
		sb.WriteString("\nNote: PHP-FPM must be restarted (restartService) to pick up changes.")
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// setPHPSettings — writes PHP ini settings.
func registerSetPHPSettingsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("setPHPSettings",
		mcp.WithDescription("Update PHP ini settings managed by devctl. Changes apply to all installed PHP versions. Restart the relevant php-fpm-{version} service afterwards to apply."),
		mcp.WithString("memory_limit",
			mcp.Description("PHP memory_limit (e.g. '256M', '512M', '1G'). Leave empty to keep current value."),
		),
		mcp.WithString("upload_max_filesize",
			mcp.Description("PHP upload_max_filesize (e.g. '64M', '128M'). Leave empty to keep current value."),
		),
		mcp.WithString("post_max_size",
			mcp.Description("PHP post_max_size (e.g. '64M', '128M'). Leave empty to keep current value."),
		),
		mcp.WithString("max_execution_time",
			mcp.Description("PHP max_execution_time in seconds (e.g. '60', '300'). Leave empty to keep current value."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Read current settings first to use as defaults for any omitted fields.
		current, err := c.getPHPSettings()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read current PHP settings: %v", err)), nil
		}
		newSettings := phpSettings{
			MemoryLimit:       req.GetString("memory_limit", current.MemoryLimit),
			UploadMaxFilesize: req.GetString("upload_max_filesize", current.UploadMaxFilesize),
			PostMaxSize:       req.GetString("post_max_size", current.PostMaxSize),
			MaxExecutionTime:  req.GetString("max_execution_time", current.MaxExecutionTime),
		}
		updated, err := c.setPHPSettings(newSettings)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update PHP settings: %v", err)), nil
		}
		var sb strings.Builder
		sb.WriteString("PHP settings updated (all installed PHP versions):\n\n")
		sb.WriteString(fmt.Sprintf("  memory_limit:        %s\n", updated.MemoryLimit))
		sb.WriteString(fmt.Sprintf("  upload_max_filesize: %s\n", updated.UploadMaxFilesize))
		sb.WriteString(fmt.Sprintf("  post_max_size:       %s\n", updated.PostMaxSize))
		sb.WriteString(fmt.Sprintf("  max_execution_time:  %s\n", updated.MaxExecutionTime))
		sb.WriteString("\nRemember to restart the relevant PHP-FPM service (restartService) to apply the new settings.")
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getSettings — reads devctl settings.
func registerGetSettingsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getSettings",
		mcp.WithDescription("Get all devctl settings: ports, DNS config, TLD, and more."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		settings, err := c.getSettings()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get settings: %v", err)), nil
		}
		var sb strings.Builder
		sb.WriteString("devctl settings:\n\n")
		keys := []string{"devctl_host", "devctl_port", "dump_tcp_port", "service_poll_interval", "mailpit_http_port", "mailpit_smtp_port", "dns_port", "dns_target_ip", "dns_tld"}
		for _, k := range keys {
			if v, ok := settings[k]; ok {
				sb.WriteString(fmt.Sprintf("  %-25s %s\n", k+":", v))
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// setSettings — writes devctl settings.
func registerSetSettingsTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("setSettings",
		mcp.WithDescription("Update devctl settings. Only provide the keys you want to change. Valid keys: devctl_host, devctl_port, dump_tcp_port, service_poll_interval, mailpit_http_port, mailpit_smtp_port, dns_port, dns_target_ip, dns_tld."),
		mcp.WithString("devctl_host",
			mcp.Description("Host devctl listens on (default: 127.0.0.1)"),
		),
		mcp.WithString("devctl_port",
			mcp.Description("Port devctl listens on (default: 4000)"),
		),
		mcp.WithString("dump_tcp_port",
			mcp.Description("TCP port for php_dd() dump receiver (default: 9912)"),
		),
		mcp.WithString("service_poll_interval",
			mcp.Description("How often services are polled for status in seconds (default: 5)"),
		),
		mcp.WithString("mailpit_http_port",
			mcp.Description("Mailpit HTTP UI port (default: 8025)"),
		),
		mcp.WithString("mailpit_smtp_port",
			mcp.Description("Mailpit SMTP port (default: 1025)"),
		),
		mcp.WithString("dns_port",
			mcp.Description("DNS server port (default: 5353)"),
		),
		mcp.WithString("dns_target_ip",
			mcp.Description("IP address *.test domains resolve to (default: 127.0.0.1)"),
		),
		mcp.WithString("dns_tld",
			mcp.Description("TLD for local sites (default: test)"),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		updates := map[string]string{}
		keys := []string{"devctl_host", "devctl_port", "dump_tcp_port", "service_poll_interval", "mailpit_http_port", "mailpit_smtp_port", "dns_port", "dns_target_ip", "dns_tld"}
		for _, k := range keys {
			if v := req.GetString(k, ""); v != "" {
				updates[k] = v
			}
		}
		if len(updates) == 0 {
			return mcp.NewToolResultError("No settings provided. Specify at least one setting key to update."), nil
		}
		if err := c.setSettings(updates); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to update settings: %v", err)), nil
		}
		var sb strings.Builder
		sb.WriteString("Settings updated:\n\n")
		for k, v := range updates {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
		sb.WriteString("\nSome settings (e.g. port changes) require restarting devctl to take effect.")
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// checkDNSSetup — checks if systemd-resolved DNS is configured.
func registerCheckDNSSetupTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("checkDNSSetup",
		mcp.WithDescription("Check whether the systemd-resolved DNS drop-in is configured so that *.test domains resolve to this machine."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		configured, err := c.checkDNSSetup()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to check DNS setup: %v", err)), nil
		}
		if configured {
			return mcp.NewToolResultText("DNS is configured. systemd-resolved has a drop-in that routes *.test queries to devctl's DNS server."), nil
		}
		return mcp.NewToolResultText("DNS is NOT configured. *.test domains will not resolve. Use configureDNS to set it up."), nil
	})
}

// configureDNS — writes the systemd-resolved drop-in.
func registerConfigureDNSTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("configureDNS",
		mcp.WithDescription("Configure systemd-resolved to route *.test DNS queries to devctl's DNS server. This writes a drop-in config and restarts systemd-resolved."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := c.configureDNS(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to configure DNS: %v", err)), nil
		}
		return mcp.NewToolResultText("DNS configured successfully. systemd-resolved now routes *.test queries to devctl's DNS server. *.test sites should resolve in your browser."), nil
	})
}

// teardownDNS — removes the systemd-resolved drop-in.
func registerTeardownDNSTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("teardownDNS",
		mcp.WithDescription("Remove the systemd-resolved DNS drop-in for *.test domains and restart systemd-resolved. After this, *.test sites will no longer resolve."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := c.teardownDNS(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to teardown DNS: %v", err)), nil
		}
		return mcp.NewToolResultText("DNS teardown complete. The systemd-resolved drop-in has been removed. *.test domains will no longer resolve."), nil
	})
}

// trustCA — installs Caddy's internal CA into the system and browser trust stores.
func registerTrustCATool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("trustCA",
		mcp.WithDescription("Trust Caddy's internal CA certificate in the system and browser trust stores. This eliminates 'certificate not trusted' warnings for *.test HTTPS sites. Requires libnss3-tools to be installed."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		output, err := c.trustCA()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to trust CA: %v", err)), nil
		}
		var sb strings.Builder
		sb.WriteString("Caddy's internal CA has been trusted.\n")
		sb.WriteString("HTTPS sites with *.test domains should no longer show certificate warnings.\n")
		if output != "" {
			sb.WriteString("\nOutput:\n")
			sb.WriteString(output)
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// ---- helpers ----

func orUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func statusIcon(status string) string {
	switch status {
	case "running":
		return "[running]"
	case "stopped":
		return "[stopped]"
	case "pending":
		return "[pending]"
	case "warning":
		return "[warning]"
	default:
		return "[unknown]"
	}
}

func filterSuffix(domain string) string {
	if domain != "" {
		return fmt.Sprintf(" for domain %q", domain)
	}
	return ""
}

// listMail — lists recent emails from Mailpit.
func registerListMailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("listEmails",
		mcp.WithDescription("List recent emails captured by Mailpit. Returns sender, recipients, subject, and read status. Use getEmail to read the full body of a specific email."),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of emails to return (default: 20, max: 100)."),
		),
		mcp.WithNumber("start",
			mcp.Description("Offset for pagination (default: 0)."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := req.GetInt("limit", 20)
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		start := req.GetInt("start", 0)
		if start < 0 {
			start = 0
		}

		resp, err := c.listMail(limit, start)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to list emails: %v", err)), nil
		}
		if len(resp.Messages) == 0 {
			return mcp.NewToolResultText("No emails found in Mailpit."), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d email(s) (total: %d, unread: %d):\n\n", len(resp.Messages), resp.Total, resp.Unread))
		for _, m := range resp.Messages {
			readStatus := "read"
			if !m.Read {
				readStatus = "UNREAD"
			}
			to := formatAddresses(m.To)
			sb.WriteString(fmt.Sprintf("• [%s] %s\n", readStatus, m.Subject))
			sb.WriteString(fmt.Sprintf("  ID: %s\n", m.ID))
			sb.WriteString(fmt.Sprintf("  From: %s | To: %s\n", formatAddress(m.From), to))
			sb.WriteString(fmt.Sprintf("  Received: %s | Size: %d bytes\n\n", m.Created, m.Size))
		}
		sb.WriteString("Use getEmail(id) to read the full body of any email.")
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// getEmail — retrieves the full content of a single email by ID.
func registerGetMailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("getEmail",
		mcp.WithDescription("Get the full content of a single email captured by Mailpit, including the plain-text and HTML body. Use listEmails first to find the email ID."),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("The email ID from listEmails (e.g. the ID field)."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		msg, err := c.getMail(id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get email %q: %v", id, err)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Subject: %s\n", msg.Subject))
		sb.WriteString(fmt.Sprintf("From:    %s\n", formatAddress(msg.From)))
		sb.WriteString(fmt.Sprintf("To:      %s\n", formatAddresses(msg.To)))
		if len(msg.Cc) > 0 {
			sb.WriteString(fmt.Sprintf("Cc:      %s\n", formatAddresses(msg.Cc)))
		}
		sb.WriteString(fmt.Sprintf("Date:    %s\n", msg.Created))
		sb.WriteString(fmt.Sprintf("ID:      %s\n", msg.ID))
		sb.WriteString("\n")

		if msg.Text != "" {
			sb.WriteString("--- Plain Text ---\n")
			sb.WriteString(msg.Text)
			sb.WriteString("\n")
		}
		if msg.HTML != "" && msg.Text == "" {
			// Only show HTML if there is no plain-text version.
			sb.WriteString("--- HTML Body ---\n")
			body := msg.HTML
			if len(body) > 4000 {
				body = body[:4000] + "\n[...truncated]"
			}
			sb.WriteString(body)
			sb.WriteString("\n")
		}
		return mcp.NewToolResultText(sb.String()), nil
	})
}

// deleteEmails — deletes specific emails by ID.
func registerDeleteMailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("deleteEmails",
		mcp.WithDescription("Delete one or more emails from Mailpit by their IDs. Use listEmails to find the IDs. To delete all emails at once, use deleteAllEmails instead."),
		mcp.WithString("ids",
			mcp.Required(),
			mcp.Description("Comma-separated list of email IDs to delete (e.g. 'abc123,def456')."),
		),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idsStr, err := req.RequireString("ids")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var ids []string
		for _, id := range strings.Split(idsStr, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return mcp.NewToolResultError("No valid IDs provided."), nil
		}

		if err := c.deleteMail(ids); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete emails: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Deleted %d email(s).", len(ids))), nil
	})
}

// deleteAllEmails — deletes every email in Mailpit.
func registerDeleteAllMailTool(s *server.MCPServer, c *client) {
	tool := mcp.NewTool("deleteAllEmails",
		mcp.WithDescription("Delete all emails from Mailpit. This is irreversible."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := c.deleteAllMail(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete all emails: %v", err)), nil
		}
		return mcp.NewToolResultText("All emails have been deleted from Mailpit."), nil
	})
}

// ---- helpers ----

func formatAddress(a mailAddress) string {
	if a.Name != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Address)
	}
	return a.Address
}

func formatAddresses(addrs []mailAddress) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, formatAddress(a))
	}
	return strings.Join(parts, ", ")
}
