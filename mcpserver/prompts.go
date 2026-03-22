package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerPrompts(s *server.MCPServer) {
	registerDiagnoseSitePrompt(s)
	registerEnableProfilingPrompt(s)
	registerServiceHealthCheckPrompt(s)
}

// DiagnoseSiteIssue — walk the agent through diagnosing a broken site.
func registerDiagnoseSitePrompt(s *server.MCPServer) {
	s.AddPrompt(
		mcp.NewPrompt("DiagnoseSiteIssue",
			mcp.WithPromptDescription("Diagnose why a local dev site is broken or behaving unexpectedly. The assistant will check PHP version compatibility, service health, and recent logs."),
			mcp.WithArgument("domain",
				mcp.ArgumentDescription("The site domain experiencing the issue (e.g. myapp.test)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("symptom",
				mcp.ArgumentDescription("Brief description of the problem (e.g. '500 error', 'blank page', 'slow response', 'missing emails')"),
			),
		),
		func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			domain := req.Params.Arguments["domain"]
			if domain == "" {
				return nil, fmt.Errorf("domain is required")
			}
			symptom := req.Params.Arguments["symptom"]
			if symptom == "" {
				symptom = "an unspecified issue"
			}

			instructions := fmt.Sprintf(`You are helping diagnose a problem with the local dev site "%s".

The reported symptom is: %s

Follow these steps in order:

1. Call getSiteDetail(domain: "%s") to get the site's current PHP version, framework, SPX state, and HTTPS config.

2. Call listServices() to check if all required services (Caddy, PHP-FPM for the site's PHP version, and any framework-specific services like Redis/MySQL/PostgreSQL) are running.

3. If any critical service is stopped, explain what it does and ask the user if they want you to start it using startService() or restart it using restartService().

4. Call listLogs() to see which log files are available, then call getLogTail() for relevant logs (caddy, php-fpm-{version}) to check for recent errors.

5. Based on the framework detected:
   - Laravel/Symfony: confirm PHP >= 8.1; suggest checking queue workers if symptom involves jobs/mail.
   - WordPress: confirm PHP >= 7.4.
   - Mention any obvious PHP version mismatches between the site config and what the framework requires.

6. Summarise your findings and suggest the most likely fix. If switching PHP versions would help, propose the specific version and ask for confirmation before calling switchPHPVersion().

7. If the problem involves profiling/performance, offer to enable SPX by calling toggleSPXProfiler(domain: "%s", action: "enable").

Always explain what you found and what you are about to do before calling any write tool. Never restart services or switch PHP versions without user confirmation.`,
				domain, symptom, domain, domain)

			return mcp.NewGetPromptResult(
				fmt.Sprintf("Diagnosing %s: %s", domain, symptom),
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
				},
			), nil
		},
	)
}

// EnableProfiling — guide the user through enabling SPX and capturing a profile.
func registerEnableProfilingPrompt(s *server.MCPServer) {
	s.AddPrompt(
		mcp.NewPrompt("EnableProfiling",
			mcp.WithPromptDescription("Enable SPX profiling for a site and guide the user on how to capture a profile and view the flamegraph in devctl."),
			mcp.WithArgument("domain",
				mcp.ArgumentDescription("The site domain to profile (e.g. myapp.test)"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("endpoint",
				mcp.ArgumentDescription("The specific URL path or endpoint you want to profile (e.g. /checkout, /api/orders)"),
			),
		),
		func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			domain := req.Params.Arguments["domain"]
			if domain == "" {
				return nil, fmt.Errorf("domain is required")
			}
			endpoint := req.Params.Arguments["endpoint"]
			if endpoint == "" {
				endpoint = "/your-endpoint"
			}

			instructions := fmt.Sprintf(`You are helping profile the local dev site "%s".

Target endpoint: %s

Follow these steps:

1. Call getSiteDetail(domain: "%s") to check the current SPX state and PHP version.

2. If SPX is already enabled (spx_enabled: 1), skip to step 4.

3. Call toggleSPXProfiler(domain: "%s", action: "enable") to enable SPX. Explain to the user that this rewrites the PHP config and restarts PHP-FPM for the site — this may take 1–2 seconds.

4. Instruct the user to trigger a profile capture:
   - Add query parameters: https://%s%s?SPX_ENABLED=1&SPX_KEY=dev
   - Or set cookies: SPX_ENABLED=1 and SPX_KEY=dev (useful for POST requests)
   - Reproduce the operation they want to measure.

5. After they confirm they triggered the request, tell them to view the flamegraph:
   - Open http://127.0.0.1:4000 in the browser
   - Navigate to the Profiler tab
   - Click the most recent profile for %s

6. Optionally call getSPXProfiles(domain: "%s") to confirm a profile was captured and show them the wall time and peak memory.

7. Offer to disable SPX after profiling is done with toggleSPXProfiler(domain: "%s", action: "disable") — leaving it on can slow all requests for that site.`,
				domain, endpoint, domain, domain, domain, endpoint, domain, domain, domain)

			return mcp.NewGetPromptResult(
				fmt.Sprintf("Profiling %s%s", domain, endpoint),
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
				},
			), nil
		},
	)
}

// ServiceHealthCheck — check all services and fix any that are down.
func registerServiceHealthCheckPrompt(s *server.MCPServer) {
	s.AddPrompt(
		mcp.NewPrompt("ServiceHealthCheck",
			mcp.WithPromptDescription("Check the health of all devctl-managed services and identify any that are stopped, in a warning state, or have updates available. Offer to restart unhealthy services."),
			mcp.WithArgument("focus",
				mcp.ArgumentDescription("Optional: limit focus to services related to a specific task (e.g. 'email', 'queue', 'database', 'cache')"),
			),
		),
		func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			focus := req.Params.Arguments["focus"]
			focusClause := ""
			if focus != "" {
				focusClause = fmt.Sprintf("\nPay special attention to services related to: %s\n", focus)
			}

			instructions := fmt.Sprintf(`You are performing a health check on all devctl-managed local dev services.
%s
Steps:

1. Call listServices() to get the current status of all installed services.

2. Identify and report:
   - Services with status "stopped" that are required or that the user likely needs
   - Services with status "warning" (running but health check failed)
   - Services with updates available (update_available: true)
   - Services with status "unknown" (might indicate a configuration problem)

3. For each unhealthy service, explain what the service does and what might break without it:
   - caddy: all *.test sites will be unreachable
   - php-fpm-{version}: sites using that PHP version will throw 502 errors
   - redis/valkey: queue workers, session caching, and broadcast (Reverb) will fail
   - mysql/postgres: database-backed apps will throw connection errors
   - mailpit: outbound emails from apps won't be captured/testable
   - meilisearch/typesense: search features in apps will fail
   - dns: *.test domains won't resolve (browser will show DNS error)

4. For any stopped service the user likely wants running, ask for confirmation and then call startService(service_id: "<id>") or restartService(service_id: "<id>") as appropriate.

5. If any service is misbehaving, call listLogs() then getLogTail(log_id: "<id>") to check recent log output for errors before suggesting a fix.

6. Summarise: all-green services, fixed services, and any services you recommend the user start or update.

Never restart services automatically — always explain what you are about to do and wait for user confirmation.`,
				focusClause)

			return mcp.NewGetPromptResult(
				"devctl service health check",
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(instructions)),
				},
			), nil
		},
	)
}
