package api

import (
	"net/http"
	"os/exec"
	"time"
)

// handleRestart responds immediately then schedules a `systemctl restart devctl`
// so the HTTP response reaches the client before the process is replaced.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "restarting"})

	// Flush the response before we schedule the restart.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := exec.Command("systemctl", "restart", "devctl").Run(); err != nil {
			// If systemctl fails (e.g. not running as a unit), fall back to nothing —
			// the log line will appear in journalctl for debugging.
			_ = err
		}
	}()
}
