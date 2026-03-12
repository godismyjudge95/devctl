package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/services"
)

func (s *Server) handleGetPHPVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := php.InstalledVersions()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if versions == nil {
		versions = []php.Version{}
	}
	// Annotate each version with its live status from the supervisor.
	for i, v := range versions {
		id := php.FPMServiceID(v.Version)
		if s.supervisor.IsRunning(id) {
			versions[i].Status = string(services.StatusRunning)
		} else {
			versions[i].Status = string(services.StatusStopped)
		}
	}
	writeJSON(w, versions)
}

func (s *Server) handleInstallPHP(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}

	var body struct {
		Extensions []string `json:"extensions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Extensions) == 0 {
		body.Extensions = php.DefaultExtensions
	}

	if err := php.Install(r.Context(), ver, body.Extensions); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Register a supervised definition and start FPM for the new version.
	def := phpFPMServiceDef(ver)
	s.registry.Register(def)
	if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
		if err := s.supervisor.Start(def); err != nil {
			// Non-fatal: log but don't fail the install response.
			_ = err
		}
	}

	versions, _ := php.InstalledVersions()
	if versions == nil {
		versions = []php.Version{}
	}
	for i, v := range versions {
		id := php.FPMServiceID(v.Version)
		if s.supervisor.IsRunning(id) {
			versions[i].Status = string(services.StatusRunning)
		} else {
			versions[i].Status = string(services.StatusStopped)
		}
	}
	writeJSON(w, versions)
}

func (s *Server) handleUninstallPHP(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}

	// Stop and unregister the supervised process first.
	id := php.FPMServiceID(ver)
	_ = s.supervisor.Stop(id)
	s.registry.Unregister(id)

	if err := php.Uninstall(r.Context(), ver); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMStart(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	def := phpFPMServiceDef(ver)
	if err := s.supervisor.Start(def); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMStop(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	if err := s.supervisor.Stop(php.FPMServiceID(ver)); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePHPFPMRestart(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}
	def := phpFPMServiceDef(ver)
	if err := s.supervisor.Restart(def); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPHPSettings(w http.ResponseWriter, r *http.Request) {
	versions, err := php.InstalledVersions()
	if err != nil || len(versions) == 0 {
		// Return defaults if no PHP installed.
		writeJSON(w, php.GlobalSettings{
			UploadMaxFilesize: "128M",
			MemoryLimit:       "256M",
			MaxExecutionTime:  "120",
			PostMaxSize:       "128M",
		})
		return
	}

	settings, err := php.GetSettings(versions[0].Version)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleSetPHPSettings(w http.ResponseWriter, r *http.Request) {
	var settings php.GlobalSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	errs := php.ApplySettings(r.Context(), settings)
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		writeError(w, strings.Join(msgs, "; "), http.StatusInternalServerError)
		return
	}

	writeJSON(w, settings)
}

// phpFPMServiceDef builds the supervised Definition for a PHP-FPM version.
// Kept here so the API handlers don't need to import main.
func phpFPMServiceDef(ver string) services.Definition {
	return services.Definition{
		ID:          php.FPMServiceID(ver),
		Label:       "PHP " + ver + " FPM",
		Managed:     true,
		ManagedCmd:  php.FPMBinary(ver),
		ManagedArgs: fmt.Sprintf("--nodaemonize --fpm-config /etc/php/%s/fpm/php-fpm.conf", ver),
		ManagedDir:  "/etc/php/" + ver + "/fpm",
	}
}
