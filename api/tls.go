package api

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
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
	caddyBin := filepath.Join(paths.ServiceDir(s.serverRoot, "caddy"), "caddy")

	var log bytes.Buffer

	// Resolve the site user's home directory so NSS db operations target the
	// right user (~/.pki/nssdb). devctl runs as root; the browser runs as
	// siteUser, so we need that user's NSS store.
	homeDir, err := siteUserHome(s.siteUser)
	if err != nil {
		writeError(w, fmt.Sprintf("could not resolve home for %q: %v", s.siteUser, err), http.StatusInternalServerError)
		return
	}

	// Ensure libnss3-tools (provides certutil) is installed.
	if _, err := exec.LookPath("certutil"); err != nil {
		log.WriteString("Installing libnss3-tools...\n")
		cmd := exec.CommandContext(r.Context(), "apt-get", "install", "-y", "libnss3-tools")
		cmd.Stdout = &log
		cmd.Stderr = &log
		if err := cmd.Run(); err != nil {
			writeError(w, "apt-get install libnss3-tools failed: "+log.String(), http.StatusInternalServerError)
			return
		}
	}

	// Ensure the NSS database exists for the site user.
	nssDir := filepath.Join(homeDir, ".pki", "nssdb")
	if _, err := os.Stat(nssDir); os.IsNotExist(err) {
		log.WriteString("Initialising NSS database...\n")
		if err := os.MkdirAll(nssDir, 0700); err != nil {
			writeError(w, "mkdir ~/.pki/nssdb: "+err.Error(), http.StatusInternalServerError)
			return
		}
		cmd := exec.CommandContext(r.Context(), "certutil", "-d", "sql:"+nssDir, "-N", "--empty-password")
		cmd.Env = append(os.Environ(), "HOME="+homeDir)
		cmd.Stdout = &log
		cmd.Stderr = &log
		if err := cmd.Run(); err != nil {
			writeError(w, "certutil -N failed: "+log.String(), http.StatusInternalServerError)
			return
		}
	}

	// Run `caddy untrust` first to clear any stale "already trusted" state that
	// would cause `caddy trust` to skip the NSS database update.
	log.WriteString("Running caddy untrust...\n")
	untrustCmd := exec.CommandContext(r.Context(), caddyBin, "untrust", "--address", adminAddr)
	untrustCmd.Env = append(os.Environ(), "HOME="+homeDir)
	untrustCmd.Stdout = &log
	untrustCmd.Stderr = &log
	// Ignore untrust errors — it fails if not yet trusted, which is fine.
	_ = untrustCmd.Run()

	// Run `caddy trust` — this updates both the system CA store and the NSS db.
	log.WriteString("Running caddy trust...\n")
	trustCmd := exec.CommandContext(r.Context(), caddyBin, "trust", "--address", adminAddr)
	trustCmd.Env = append(os.Environ(), "HOME="+homeDir)
	trustCmd.Stdout = &log
	trustCmd.Stderr = &log
	if err := trustCmd.Run(); err != nil {
		writeError(w, "caddy trust failed: "+log.String(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "trusted", "output": log.String()})
}

// siteUserHome returns the home directory of the named OS user.
func siteUserHome(username string) (string, error) {
	if username == "" {
		return os.UserHomeDir()
	}
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
