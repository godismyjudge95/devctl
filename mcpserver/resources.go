package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerResources(s *server.MCPServer, c *client) {
	// devctl://sites — list of all sites
	s.AddResource(
		mcp.NewResource(
			"devctl://sites",
			"All devctl sites",
			mcp.WithResourceDescription("All sites managed by devctl — domain, root path, PHP version, framework, and SPX/profiler state."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			sites, err := c.listSites()
			if err != nil {
				return nil, err
			}
			b, _ := json.MarshalIndent(sites, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "devctl://sites",
					MIMEType: "application/json",
					Text:     string(b),
				},
			}, nil
		},
	)

	// devctl://services — live status of all services
	s.AddResource(
		mcp.NewResource(
			"devctl://services",
			"Service statuses",
			mcp.WithResourceDescription("Live status of all devctl-managed services: Caddy, PHP-FPM instances, Redis, PostgreSQL, MySQL, Mailpit, Meilisearch, Typesense, Reverb, etc."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			svcs, err := c.listServices()
			if err != nil {
				return nil, err
			}
			b, _ := json.MarshalIndent(svcs, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "devctl://services",
					MIMEType: "application/json",
					Text:     string(b),
				},
			}, nil
		},
	)

	// devctl://php — installed PHP versions and global PHP settings
	s.AddResource(
		mcp.NewResource(
			"devctl://php",
			"PHP versions & global settings",
			mcp.WithResourceDescription("Installed PHP versions and their FPM status, plus global PHP settings (memory_limit, upload_max_filesize, max_execution_time, post_max_size)."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			versions, err := c.listPHPVersions()
			if err != nil {
				return nil, err
			}
			settings, err := c.getPHPSettings()
			if err != nil {
				return nil, err
			}
			out := map[string]any{
				"versions": versions,
				"settings": settings,
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "devctl://php",
					MIMEType: "application/json",
					Text:     string(b),
				},
			}, nil
		},
	)

	// devctl://dumps — most recent php_dd() dumps
	s.AddResource(
		mcp.NewResource(
			"devctl://dumps",
			"Recent php_dd() dumps",
			mcp.WithResourceDescription("The 20 most recent php_dd() variable dumps captured by devctl from all sites."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			resp, err := c.listDumps("")
			if err != nil {
				return nil, err
			}
			b, _ := json.MarshalIndent(resp, "", "  ")
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "devctl://dumps",
					MIMEType: "application/json",
					Text:     string(b),
				},
			}, nil
		},
	)

	// devctl://sites/{domain} — detail for a single site by domain
	s.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"devctl://sites/{domain}",
			"Site detail",
			mcp.WithTemplateDescription("Full detail for a single devctl site, looked up by domain name (e.g. myapp.test)."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			// Extract domain from URI: devctl://sites/<domain>
			uri := req.Params.URI
			const prefix = "devctl://sites/"
			if len(uri) <= len(prefix) {
				return nil, fmt.Errorf("domain is required")
			}
			domain := uri[len(prefix):]

			sites, err := c.listSites()
			if err != nil {
				return nil, err
			}
			for _, st := range sites {
				if st.Domain == domain {
					b, _ := json.MarshalIndent(st, "", "  ")
					return []mcp.ResourceContents{
						mcp.TextResourceContents{
							URI:      req.Params.URI,
							MIMEType: "application/json",
							Text:     string(b),
						},
					}, nil
				}
			}
			return nil, fmt.Errorf("no site found with domain %q", domain)
		},
	)
}
