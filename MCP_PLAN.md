Here's a clear, step-by-step **plan** for building an effective **AI agent** (or agentic workflow) that leverages **devctl** through an MCP server. This focuses purely on the conceptual design—no code, no implementation details—just the strategy, reasoning, scope, and flow so you can see how it would feel useful in practice for a developer using modern LLM-based IDEs/tools (Claude Desktop, Cursor, Windsurf, VS Code with Copilot, etc.).

### Overall Goal of the Agent
Create a **"devctl-aware local dev assistant"** that acts as an extension of your devctl dashboard.  
The agent doesn't try to replace devctl's UI or do things the AI can already handle (like running git, composer, or creating folders). Instead, it uses devctl's privileged, centralized knowledge and controls to handle the parts that are annoying, error-prone, or hidden when you're just in a terminal/IDE:  
- Quickly understanding your local environment (sites, versions, statuses)  
- Making safe, targeted configuration changes (especially PHP + SPX)  
- Accessing observability (logs, profiler data, dumps) without manual tailing or digging  
- Triaging common local dev problems (version mismatches, service hiccups, slow code)

The agent stays **read-mostly + controlled writes**, respecting your philosophy: auto-detection rules, no blind command execution, no site creation/deletion.

### Core MCP Components the Agent Will Use
- **Resources** (read-only context the AI pulls automatically or on-demand)  
  - Current list & summary of all sites (name, path, detected type, PHP version, status, URL, git worktree info if applicable)  
  - Global service statuses (Caddy, DNS, databases, queues, Mailpit, etc.)  
  - Installed PHP versions overview  
  - Recent dump arrivals (php_dd() captures)  

- **Tools** (callable actions the agent executes when it decides they're needed)  
  - Query detailed site info (PHP config, FPM status, SPX state, recent errors)  
  - Switch PHP version for a specific site (with restart)  
  - Enable/disable SPX profiler for a site (with optional trigger config)  
  - Fetch recent logs for a service or site-related process  
  - Restart a specific service (Caddy, MariaDB/MySQL, Redis/Valkey, etc.)  
  - (Optional low-risk) Read-only snapshot of non-sensitive .env keys for a site  

- **Prompts** (reusable templates that guide the LLM how to use the above effectively)  
  - "DiagnoseSiteIssue" — Pull site details → check logs/status → suggest PHP switch or restart  
  - "EnableProfiling" — Confirm site → toggle SPX → explain how to trigger (cookie/param) → remind to view in devctl UI  
  - "VersionConsistencyCheck" — Compare site's PHP vs framework requirements (from composer.json or docs) → recommend switch  
  - "ServiceHealthCheck" — List services → focus on ones related to the current task → restart if unhealthy  

### How the Agent Workflow Would Feel (Step-by-Step Scenarios)
1. **You say:** "Why is my Laravel app throwing 500s on this endpoint?"  
   → Agent uses resources to list sites → identifies your current/mentioned site → tool: getSiteDetails → checks PHP version, status → tool: getServiceLogs (Caddy + PHP-FPM) → tool: getSiteEnv summary (debug mode?) → reasons: "PHP 8.2 mismatch suspected; Laravel 11 wants 8.3+" → suggests + executes switchPhpVersion if you confirm → follows up with "Restarted FPM; retry the request and check logs again."

2. **You say:** "This page is slow—help me profile it."  
   → Agent pulls site list/resource → tool: toggleSPXProfiler (enable) → outputs: "SPX now active. Add ?SPX_ENABLED=1&SPX_KEY=dev to the URL (or cookie), reproduce the slow request, then view flamegraph/timeline in devctl's Profiler tab at http://127.0.0.1:4000."  
   → (Later) You paste profiler observations → agent analyzes and suggests fixes.

3. **You say:** "Mailpit isn't catching emails from my app."  
   → Resources show service statuses → tool: getServiceLogs (Mailpit) → tool: restartService if crashed → confirms SMTP config via env read if needed.

4. **General "what's my setup?" query**  
   → Agent auto-pulls resources (listSites + listServices) → summarizes in natural language: "You have 7 sites auto-detected in ~/sites. Active PHP versions: 8.2 (3 sites), 8.3 (4 sites). All core services green except Meilisearch stopped. Two sites have pending dumps from php_dd()."

### Prioritized Rollout Phases (Conceptual)
**Phase 1 – Quick Value (MVP – focus here first)**  
Expose read-heavy resources + 3–4 high-impact tools:  
- listSites / site summaries  
- getSiteDetails  
- switchPhpVersion  
- toggleSPXProfiler  
Add 2 prompts: DiagnoseSiteIssue + EnableProfiling  
→ Covers 70–80% of common debugging sessions.

**Phase 2 – Observability Boost**  
Add log fetching + service status/restart tools  
Add ServiceHealthCheck prompt  
→ Makes the agent great for "why is X down?" or "fix my queue" moments.

**Phase 3 – Polish & Safety**  
Read-only env snapshots (filtered)  
More prompts (VersionConsistencyCheck, DumpArrivalAlert)  
Optional: notification hooks if devctl supports SSE/websockets for new dumps/mails (agent could subscribe via resource updates).

### Key Design Principles for This Agent
- **Never guess paths/versions** — always ask devctl via MCP.  
- **No destructive actions** — no deletes, no arbitrary exec, no site creation.  
- **Human-in-the-loop for writes** — agent proposes (e.g., "I recommend switching to PHP 8.4—confirm?") rather than auto-running dangerous stuff.  
- **Complement the dashboard** — agent directs you back to UI for visuals (flamegraphs, WhoDB, config editor).  
- **Local-first & private** — everything stays on your machine; no cloud leak.

This turns devctl + MCP into a seamless "second brain" for your local PHP environment—something the AI reaches for automatically when you mention a site, error, or performance issue.

If this direction feels right, we can refine it: pick a narrower starting scope (e.g., just PHP version + SPX agent), add more scenarios, or adjust priorities based on what pains you most day-to-day. What part excites you most, or what would you tweak?
