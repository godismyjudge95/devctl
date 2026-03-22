package mcpserver

import (
	"github.com/mark3labs/mcp-go/server"
)

const version = "0.2.0"

// New creates an MCPServer wired with all devctl resources, tools, and prompts.
// apiAddr is the host:port of the devctl HTTP API (e.g. "127.0.0.1:4000").
// The returned *server.StreamableHTTPServer implements http.Handler and can be
// mounted directly on devctl's mux at /mcp.
func New(apiAddr string) *server.StreamableHTTPServer {
	c := newClient(apiAddr)

	s := server.NewMCPServer(
		"devctl",
		version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, false),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
	)

	registerResources(s, c)
	registerTools(s, c)
	registerPrompts(s)

	return server.NewStreamableHTTPServer(s)
}
