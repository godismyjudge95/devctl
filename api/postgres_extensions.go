package api

import (
	"io"
	"net/http"

	"github.com/danielgormly/devctl/install"
)

// handleGetPostgresExtensions lists managed PostgreSQL extensions and their status.
// GET /api/postgres/extensions
func (s *Server) handleGetPostgresExtensions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, install.ListPostgresExtensions(s.serverRoot, s.siteUser))
}

// handleEnsurePostgresExtensions re-runs install-files + wire for managed extensions.
// POST /api/postgres/extensions/ensure
func (s *Server) handleEnsurePostgresExtensions(w http.ResponseWriter, r *http.Request) {
	env := install.ExtensionEnv{
		ServerRoot: s.serverRoot,
		SiteUser:   s.siteUser,
	}
	if err := install.EnsureManagedPostgresExtensions(r.Context(), io.Discard, env); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Poll wire if postgres is accepting connections.
	if err := install.EnsurePostgresExtensionsAfterStart(s.serverRoot, s.siteUser); err != nil {
		writeJSON(w, map[string]any{
			"status":     "partial",
			"error":      err.Error(),
			"extensions": install.ListPostgresExtensions(s.serverRoot, s.siteUser),
		})
		return
	}
	writeJSON(w, map[string]any{
		"status":     "ok",
		"extensions": install.ListPostgresExtensions(s.serverRoot, s.siteUser),
	})
}

func postgresSettingsPayload(serverRoot, siteUser string) map[string]any {
	return map[string]any{
		"extensions": install.ListPostgresExtensions(serverRoot, siteUser),
	}
}
