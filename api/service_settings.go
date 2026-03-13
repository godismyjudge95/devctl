package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
)

// handleGetServiceDetails returns connection details for a service.
// Currently only php-fpm-* services return data (the FPM socket path).
// Other services return 404.
func (s *Server) handleGetServiceDetails(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if strings.HasPrefix(id, "php-fpm-") {
		ver := strings.TrimPrefix(id, "php-fpm-")
		writeJSON(w, map[string]string{
			"Socket": php.FPMSocket(ver),
		})
		return
	}

	writeError(w, fmt.Sprintf("service %q has no connection details", id), http.StatusNotFound)
}

// handleGetServiceSettings returns configurable settings for a service.
// Only mailpit and php-fpm-* are supported; others return 404.
func (s *Server) handleGetServiceSettings(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if id == "mailpit" {
		httpPort, _ := s.queries.GetSetting(r.Context(), "mailpit_http_port")
		if httpPort == "" {
			httpPort = "8025"
		}
		smtpPort, _ := s.queries.GetSetting(r.Context(), "mailpit_smtp_port")
		if smtpPort == "" {
			smtpPort = "1025"
		}
		writeJSON(w, map[string]string{
			"http_port": httpPort,
			"smtp_port": smtpPort,
		})
		return
	}

	if strings.HasPrefix(id, "php-fpm-") {
		ver := strings.TrimPrefix(id, "php-fpm-")
		settings, err := php.GetSettings(ver, s.siteHome)
		if err != nil {
			// Return sensible defaults when the ini doesn't exist yet.
			writeJSON(w, php.GlobalSettings{
				UploadMaxFilesize: "128M",
				MemoryLimit:       "256M",
				MaxExecutionTime:  "120",
				PostMaxSize:       "128M",
			})
			return
		}
		writeJSON(w, settings)
		return
	}

	writeError(w, fmt.Sprintf("service %q has no configurable settings", id), http.StatusNotFound)
}

// handlePutServiceSettings saves configurable settings for a service and
// restarts it so the new values take effect immediately.
func (s *Server) handlePutServiceSettings(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if id == "mailpit" {
		var input struct {
			HTTPPort string `json:"http_port"`
			SMTPPort string `json:"smtp_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "mailpit_http_port", Value: input.HTTPPort}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "mailpit_smtp_port", Value: input.SMTPPort}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Restart with updated ports.
		def, ok := s.registry.Get("mailpit")
		if ok {
			def = s.mailpitDef(ctx, def)
			_ = s.manager.Restart(def)
		}
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}

	if strings.HasPrefix(id, "php-fpm-") {
		ver := strings.TrimPrefix(id, "php-fpm-")
		var settings php.GlobalSettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		errs := php.ApplySettings(r.Context(), settings, s.siteHome)
		if len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			writeError(w, strings.Join(msgs, "; "), http.StatusInternalServerError)
			return
		}
		// Restart this specific FPM version.
		def := s.phpFPMServiceDef(ver)
		_ = s.supervisor.Restart(def)
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}

	writeError(w, fmt.Sprintf("service %q has no configurable settings", id), http.StatusNotFound)
}

// handleGetServicePHPConfig reads a PHP config file for a php-fpm-* service.
// This is an alias route that delegates to handleGetPHPConfig using the version
// extracted from the service ID.
func (s *Server) handleGetServicePHPConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "php-fpm-") {
		writeError(w, "config files are only available for php-fpm-* services", http.StatusBadRequest)
		return
	}
	ver := strings.TrimPrefix(id, "php-fpm-")
	file := r.PathValue("file")
	if file == "" {
		writeError(w, "file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.siteHome, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"content": string(content)})
}

// handlePutServicePHPConfig writes a PHP config file for a php-fpm-* service.
func (s *Server) handlePutServicePHPConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "php-fpm-") {
		writeError(w, "config files are only available for php-fpm-* services", http.StatusBadRequest)
		return
	}
	ver := strings.TrimPrefix(id, "php-fpm-")
	file := r.PathValue("file")
	if file == "" {
		writeError(w, "file required", http.StatusBadRequest)
		return
	}
	path, ok := configFilePath(ver, s.siteHome, file)
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
