package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/tools"
)

// shareWaitTimeout bounds how long POST /api/sites/{id}/share waits for
// cloudflared to print its URL. The tunnel keeps starting after a timeout.
const shareWaitTimeout = 45 * time.Second

// GET /api/shares
func (s *Server) handleGetShares(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.shares.List())
}

// POST /api/sites/{id}/share installs cloudflared on first use, then starts a
// quick tunnel and returns the share.
func (s *Server) handleShareSite(w http.ResponseWriter, r *http.Request) {
	site, err := s.queries.GetSite(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	binDir := paths.BinDir(s.serverRoot)
	if _, err := os.Stat(filepath.Join(binDir, tools.Cloudflared.Name)); err != nil {
		if err := tools.EnsureLatest(r.Context(), tools.Cloudflared, binDir, io.Discard); err != nil {
			writeError(w, "install cloudflared: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), shareWaitTimeout)
	defer cancel()
	sh, err := s.shares.Start(ctx, site.ID, site.Domain)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sh)
}

// DELETE /api/sites/{id}/share is idempotent: 204 whether or not a share ran.
func (s *Server) handleUnshareSite(w http.ResponseWriter, r *http.Request) {
	s.shares.Stop(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// StopShares ends every tunnel. main calls it on shutdown.
func (s *Server) StopShares() {
	s.shares.StopAll()
}

// stopChildren ends every child process before the daemon re-execs, so no
// tunnel or service outlives the old process image untracked.
func (s *Server) stopChildren() {
	s.shares.StopAll()
	s.supervisor.StopAll()
}
