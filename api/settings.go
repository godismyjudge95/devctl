package api

import (
	"context"
	"encoding/json"
	"net/http"

	dbq "github.com/danielgormly/devctl/db/queries"
)

// settingDefaults maps setting keys to their runtime fallback values,
// matching the logic in main.go's getSetting() calls.
var settingDefaults = map[string]string{
	"devctl_host":           "127.0.0.1",
	"devctl_port":           "4000",
	"dump_tcp_port":         "9912",
	"service_poll_interval": "5",
	"mailpit_http_port":     "8025",
	"mailpit_smtp_port":     "1025",
	"dns_port":              "5354",
	"dns_target_ip":         "",
	"dns_tld":               ".test",
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.GetAllSettings(context.Background())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	writeJSON(w, out)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var input map[string]string
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	for k, v := range input {
		if err := s.queries.SetSetting(context.Background(), dbq.SetSettingParams{Key: k, Value: v}); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

// handleGetResolvedSettings returns all settings with runtime fallback defaults
// applied — inputs on the Settings page always show real values.
func (s *Server) handleGetResolvedSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.GetAllSettings(context.Background())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Start with defaults, then overlay whatever is stored in the DB.
	out := make(map[string]string, len(settingDefaults))
	for k, v := range settingDefaults {
		out[k] = v
	}
	for _, row := range rows {
		if row.Value != "" {
			out[row.Key] = row.Value
		}
	}
	writeJSON(w, out)
}

// GetSetting is a helper for reading a single setting from the DB.
func (s *Server) GetSetting(key string) (string, error) {
	return s.queries.GetSetting(context.Background(), key)
}
