package api

import (
	"bytes"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
)

func (s *Server) handleTLSCert(w http.ResponseWriter, r *http.Request) {
	cert, err := s.caddy.RootCert()
	if err != nil {
		writeError(w, "caddy not available: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="devctl-ca.pem"`)
	w.WriteHeader(http.StatusOK)
	w.Write(cert)
}

func (s *Server) handleTLSTrust(w http.ResponseWriter, r *http.Request) {
	const adminAddr = "localhost:2019"

	cmd := exec.CommandContext(r.Context(), filepath.Join(paths.ServiceDir(s.siteHome, "caddy"), "caddy"), "trust", "--address", adminAddr)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		writeError(w, "caddy trust failed: "+out.String(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "trusted", "output": out.String()})
}
