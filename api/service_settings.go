package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/dnsserver"
	"github.com/danielgormly/devctl/paths"
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
// Supported: mailpit, mysql, php-fpm-*. Others return 404.
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

	if id == "mysql" {
		port, _ := s.queries.GetSetting(r.Context(), "mysql_port")
		if port == "" {
			port = "3306"
		}
		bindAddr, _ := s.queries.GetSetting(r.Context(), "mysql_bind_address")
		if bindAddr == "" {
			bindAddr = "127.0.0.1"
		}
		writeJSON(w, map[string]string{
			"port":         port,
			"bind_address": bindAddr,
		})
		return
	}

	if strings.HasPrefix(id, "php-fpm-") {
		ver := strings.TrimPrefix(id, "php-fpm-")
		settings, err := php.GetSettings(ver, s.serverRoot)
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

	if id == "dns" {
		port, _ := s.queries.GetSetting(r.Context(), "dns_port")
		if port == "" {
			port = "5354"
		}
		targetIP, _ := s.queries.GetSetting(r.Context(), "dns_target_ip")
		if targetIP == "" {
			targetIP = dnsserver.DetectLANIP()
		}
		tld, _ := s.queries.GetSetting(r.Context(), "dns_tld")
		if tld == "" {
			tld = ".test"
		}
		// Check whether the systemd-resolved drop-in is configured.
		configured := dnsSystemConfigured()
		writeJSON(w, map[string]interface{}{
			"port":                  port,
			"target_ip":             targetIP,
			"tld":                   tld,
			"system_dns_configured": configured,
		})
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

	if id == "mysql" {
		var input struct {
			Port        string `json:"port"`
			BindAddress string `json:"bind_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "mysql_port", Value: input.Port}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "mysql_bind_address", Value: input.BindAddress}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Restart with updated settings.
		def, ok := s.registry.Get("mysql")
		if ok {
			def = s.mysqlDef(ctx, def)
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
		errs := php.ApplySettings(r.Context(), settings, s.serverRoot)
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

	if id == "dns" {
		var input struct {
			Port     string `json:"port"`
			TargetIP string `json:"target_ip"`
			TLD      string `json:"tld"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "dns_port", Value: input.Port}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "dns_target_ip", Value: input.TargetIP}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.queries.SetSetting(ctx, dbq.SetSettingParams{Key: "dns_tld", Value: input.TLD}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Restart with updated settings.
		def, ok := s.registry.Get("dns")
		if ok {
			def = s.dnsDef(ctx, def)
			_ = s.manager.Restart(def)
		}
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}

	writeError(w, fmt.Sprintf("service %q has no configurable settings", id), http.StatusNotFound)
}

// handleGetServiceConfig reads a config file for a service.
// Supported: php-fpm-* (php.ini, php-fpm.conf) and mysql (my.cnf).
func (s *Server) handleGetServiceConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file := r.PathValue("file")
	if file == "" {
		writeError(w, "file required", http.StatusBadRequest)
		return
	}

	if id == "mysql" {
		path, ok := mysqlConfigFilePath(s.serverRoot, file)
		if !ok {
			writeError(w, "file must be my.cnf", http.StatusBadRequest)
			return
		}
		content, err := os.ReadFile(path)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"content": string(content)})
		return
	}

	if !strings.HasPrefix(id, "php-fpm-") {
		writeError(w, "config files are only available for php-fpm-* and mysql services", http.StatusBadRequest)
		return
	}
	ver := strings.TrimPrefix(id, "php-fpm-")
	path, ok := configFilePath(ver, s.serverRoot, file)
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

// handlePutServiceConfig writes a config file for a service and restarts it.
// Supported: php-fpm-* (php.ini, php-fpm.conf) and mysql (my.cnf).
func (s *Server) handlePutServiceConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file := r.PathValue("file")
	if file == "" {
		writeError(w, "file required", http.StatusBadRequest)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if id == "mysql" {
		path, ok := mysqlConfigFilePath(s.serverRoot, file)
		if !ok {
			writeError(w, "file must be my.cnf", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(path, []byte(body.Content), 0644); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Restart MySQL so the new config takes effect.
		def, ok := s.registry.Get("mysql")
		if ok {
			def = s.mysqlDef(r.Context(), def)
			_ = s.manager.Restart(def)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !strings.HasPrefix(id, "php-fpm-") {
		writeError(w, "config files are only available for php-fpm-* and mysql services", http.StatusBadRequest)
		return
	}
	ver := strings.TrimPrefix(id, "php-fpm-")
	path, ok := configFilePath(ver, s.serverRoot, file)
	if !ok {
		writeError(w, "file must be php.ini or php-fpm.conf", http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(path, []byte(body.Content), 0644); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mysqlConfigFilePath returns the absolute path for a mysql config file,
// validating that only my.cnf is accessible.
func mysqlConfigFilePath(serverRoot, file string) (string, bool) {
	if file != "my.cnf" {
		return "", false
	}
	return filepath.Join(paths.ServiceDir(serverRoot, "mysql"), "my.cnf"), true
}
