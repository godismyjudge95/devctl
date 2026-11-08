package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
)

type phpXdebugRequest struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
}

// handleSetPHPXdebug turns Xdebug on or off for one version through the
// managed php.ini block, and restarts only that version's FPM pool.
func (s *Server) handleSetPHPXdebug(w http.ResponseWriter, r *http.Request) {
	ver, code, err := s.resolvePHPScope(r.PathValue("ver"))
	if err == nil && ver == php.GlobalScope {
		code, err = http.StatusBadRequest, errors.New("Xdebug is set for one PHP version, not all")
	}
	if err != nil {
		writeError(w, err.Error(), code)
		return
	}
	var body phpXdebugRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, `body must be {"enabled": true|false, "mode": "debug"}`, http.StatusBadRequest)
		return
	}
	if body.Mode == "" {
		body.Mode = php.DefaultXdebugMode
	}
	if err := php.ValidateXdebugMode(body.Mode); body.Enabled && err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	st, effective, err := s.phpXdebugStatus(r.Context(), ver)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch {
	case body.Enabled && !st.Available:
		writeError(w, php.ErrXdebugUnavailable.Error()+" (PHP "+ver+")", http.StatusConflict)
		return
	case body.Enabled:
		err = s.setPHPIniDirectives(r.Context(), ver, php.XdebugOnDirectives(ver, s.serverRoot, body.Mode, effective))
	case st.Enabled:
		err = s.removePHPXdebug(r.Context(), ver)
	}
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st, _, err = s.phpXdebugStatus(r.Context(), ver)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, st)
}

func (s *Server) phpXdebugStatus(ctx context.Context, ver string) (php.XdebugStatus, map[string]string, error) {
	ds, err := listPHPIniDirectives(ctx, s.queries)
	if err != nil {
		return php.XdebugStatus{}, nil, err
	}
	effective := php.EffectiveDirectives(ds, ver)
	return php.XdebugStatusFor(ver, s.serverRoot, effective), effective, nil
}

func (s *Server) removePHPXdebug(ctx context.Context, ver string) error {
	for _, key := range php.XdebugOffKeys {
		if _, err := s.queries.DeletePHPIniDirective(ctx, dbq.DeletePHPIniDirectiveParams{Scope: ver, Key: key}); err != nil {
			return err
		}
	}
	return s.writePHPIni(ctx, ver, true)
}
