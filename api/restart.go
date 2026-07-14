package api

import (
	"net/http"
	"time"

	"github.com/danielgormly/devctl/internal/reexec"
)

// handleRestart responds immediately then re-execs the daemon process so the
// HTTP response reaches the client before the process image is replaced.
// Re-exec needs no sudo (unlike systemctl restart of a system unit).
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "restarting"})

	// Flush the response before we schedule the restart.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	reexec.Schedule(300*time.Millisecond, nil)
}
