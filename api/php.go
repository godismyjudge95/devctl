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

// configFilePath resolves the absolute path for an allowed PHP config file.
// Returns ("", false) if the filename is not in the allowlist.
func configFilePath(ver, serverRoot, file string) (string, bool) {
	switch file {
	case "php.ini":
		return php.PHPIniPath(ver, serverRoot), true
	case "php-fpm.conf":
		return php.FPMConfigPath(ver, serverRoot), true
	default:
		return "", false
	}
}

func (s *Server) handleGetPHPVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := php.InstalledVersions(s.serverRoot)
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

	if err := php.Install(r.Context(), ver, s.serverRoot, s.siteUser, s.siteHome); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Register a supervised definition and start FPM for the new version.
	def := s.phpFPMServiceDef(ver)
	s.registry.Register(def)
	if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
		if err := s.supervisor.Start(def); err != nil {
			// Non-fatal: log but don't fail the install response.
			_ = err
		}
	}

	versions, _ := php.InstalledVersions(s.serverRoot)
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

	if err := php.Uninstall(r.Context(), ver, s.serverRoot); err != nil {
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
	def := s.phpFPMServiceDef(ver)
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
	def := s.phpFPMServiceDef(ver)
	if err := s.supervisor.Restart(def); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPHPSettings(w http.ResponseWriter, r *http.Request) {
	versions, err := php.InstalledVersions(s.serverRoot)
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

	settings, err := php.GetSettings(versions[0].Version, s.serverRoot)
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

	errs := php.ApplySettings(r.Context(), settings, s.serverRoot)
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
func (s *Server) phpFPMServiceDef(ver string) services.Definition {
	return services.Definition{
		ID:           php.FPMServiceID(ver),
		Label:        "PHP " + ver + " FPM",
		Managed:      true,
		ManagedCmd:   php.FPMBinary(ver, s.serverRoot),
		ManagedArgs:  fmt.Sprintf("--nodaemonize --fpm-config %s", php.FPMConfigPath(ver, s.serverRoot)),
		ManagedDir:   php.PHPDir(ver, s.serverRoot),
		Log:          php.FPMLogPath(ver, s.serverRoot),
		Version:      php.FPMBinary(ver, s.serverRoot) + " -v",
		VersionRegex: `PHP (?P<version>[\d.]+)`,
	}
}

// handleGetPHPConfig reads a PHP config file and returns its content as JSON.
func (s *Server) handleGetPHPConfig(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	file := r.PathValue("file")
	if ver == "" || file == "" {
		writeError(w, "version and file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.serverRoot, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

// handleSetPHPConfig writes content to a PHP config file.
func (s *Server) handleSetPHPConfig(w http.ResponseWriter, r *http.Request) {
	ver := r.PathValue("ver")
	file := r.PathValue("file")
	if ver == "" || file == "" {
		writeError(w, "version and file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.serverRoot, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(path, []byte(body.Content), 0644); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
