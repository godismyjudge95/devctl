package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/danielgormly/devctl/dnsserver"
)

const (
	resolvedDropinDir  = "/etc/systemd/resolved.conf.d"
	resolvedDropinFile = "/etc/systemd/resolved.conf.d/99-devctl-dns.conf"
)

// handleDNSDetectIP returns the auto-detected primary LAN IP.
//
//	GET /api/dns/detect-ip
func (s *Server) handleDNSDetectIP(w http.ResponseWriter, r *http.Request) {
	ip := dnsserver.DetectLANIP()
	writeJSON(w, map[string]string{"ip": ip})
}

// handleDNSCheckSetup reports whether the systemd-resolved drop-in is present.
//
//	GET /api/dns/setup
func (s *Server) handleDNSCheckSetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"configured": dnsSystemConfigured()})
}

// handleDNSSetup writes the systemd-resolved drop-in and restarts resolved.
//
//	POST /api/dns/setup
func (s *Server) handleDNSSetup(w http.ResponseWriter, r *http.Request) {
	// Read current dns_port and dns_tld from DB (with defaults).
	ctx := r.Context()
	port, err := s.queries.GetSetting(ctx, "dns_port")
	if err != nil || port == "" {
		port = "5354"
	}
	tld, err := s.queries.GetSetting(ctx, "dns_tld")
	if err != nil || tld == "" {
		tld = ".test"
	}
	// Strip leading dot for the Domains= line (e.g. ".test" → "test").
	domainVal := tld
	if len(domainVal) > 0 && domainVal[0] == '.' {
		domainVal = domainVal[1:]
	}

	content := fmt.Sprintf("[Resolve]\nDNS=127.0.0.1:%s\nDomains=~%s\n", port, domainVal)

	if err := os.MkdirAll(resolvedDropinDir, 0755); err != nil {
		writeError(w, fmt.Sprintf("create drop-in dir: %v", err), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(resolvedDropinFile, []byte(content), 0644); err != nil {
		writeError(w, fmt.Sprintf("write drop-in: %v", err), http.StatusInternalServerError)
		return
	}

	if err := restartResolved(); err != nil {
		writeError(w, fmt.Sprintf("restart systemd-resolved: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// handleDNSTeardown removes the systemd-resolved drop-in and restarts resolved.
//
//	DELETE /api/dns/setup
func (s *Server) handleDNSTeardown(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(resolvedDropinFile); err != nil && !os.IsNotExist(err) {
		writeError(w, fmt.Sprintf("remove drop-in: %v", err), http.StatusInternalServerError)
		return
	}

	if err := restartResolved(); err != nil {
		writeError(w, fmt.Sprintf("restart systemd-resolved: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// dnsSystemConfigured returns true when the devctl drop-in file exists.
func dnsSystemConfigured() bool {
	_, err := os.Stat(resolvedDropinFile)
	return err == nil
}

// restartResolved runs systemctl restart systemd-resolved.
func restartResolved() error {
	cmd := exec.Command("systemctl", "restart", "systemd-resolved")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}
