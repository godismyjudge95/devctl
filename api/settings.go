package api

import (
	"context"
	"encoding/json"
	"net/http"

	dbq "github.com/danielgormly/devctl/db/queries"
)

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

// GetSetting is a helper for reading a single setting from the DB.
func (s *Server) GetSetting(key string) (string, error) {
	return s.queries.GetSetting(context.Background(), key)
}
