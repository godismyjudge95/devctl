package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgormly/devctl/php"
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

	versions, _ := php.InstalledVersions()
	writeJSON(w, versions)
}

func (s *Server) handleUninstallPHP(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	if ver == "" {
		writeError(w, "version required", http.StatusBadRequest)
		return
	}

	if err := php.Uninstall(r.Context(), ver); err != nil {
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
