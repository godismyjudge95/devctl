package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
)

// phpSettingsView is the GET/PUT /api/php/settings payload for one scope.
// Overrides lists the GlobalSettings keys that have a directive in this scope.
type phpSettingsView struct {
	php.GlobalSettings
	Scope     string   `json:"scope"`
	Overrides []string `json:"overrides"`
}

var defaultPHPSettings = php.GlobalSettings{
	UploadMaxFilesize: "128M",
	MemoryLimit:       "256M",
	MaxExecutionTime:  "120",
	PostMaxSize:       "128M",
}

// resolvePHPScope turns a ?version= or path value into a directive scope.
// "" means GlobalScope. A version must be installed.
func (s *Server) resolvePHPScope(raw string) (string, int, error) {
	if raw == "" {
		return php.GlobalScope, 0, nil
	}
	if err := php.ValidateScope(raw); err != nil {
		return "", http.StatusBadRequest, err
	}
	if raw == php.GlobalScope {
		return raw, 0, nil
	}
	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	for _, v := range versions {
		if v.Version == raw {
			return raw, 0, nil
		}
	}
	return "", http.StatusNotFound, fmt.Errorf("PHP %s is not installed", raw)
}

func listPHPIniDirectives(ctx context.Context, q *dbq.Queries) ([]php.Directive, error) {
	rows, err := q.ListPHPIniDirectives(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]php.Directive, len(rows))
	for i, r := range rows {
		out[i] = php.Directive{Scope: r.Scope, Key: r.Key, Value: r.Value}
	}
	return out, nil
}

// setPHPIniDirectives stores directives for scope, rewrites the managed
// php.ini block of every version the scope covers, and restarts their running
// FPM pools.
// Callers validate kv with php.ValidateDirective first.
func (s *Server) setPHPIniDirectives(ctx context.Context, scope string, kv map[string]string) error {
	for k, v := range kv {
		if err := s.queries.SetPHPIniDirective(ctx, dbq.SetPHPIniDirectiveParams{Scope: scope, Key: k, Value: v}); err != nil {
			return err
		}
	}
	return s.writePHPIni(ctx, scope, true)
}

// unsetPHPIniDirective removes one directive. It reports false when the
// directive was not set.
func (s *Server) unsetPHPIniDirective(ctx context.Context, scope, key string) (bool, error) {
	n, err := s.queries.DeletePHPIniDirective(ctx, dbq.DeletePHPIniDirectiveParams{Scope: scope, Key: key})
	if err != nil || n == 0 {
		return false, err
	}
	return true, s.writePHPIni(ctx, scope, true)
}

// SyncPHPIni writes the managed php.ini block for every installed version.
// Called at startup before FPM starts, so a php.ini that was deleted and
// regenerated gets its directives back.
func SyncPHPIni(ctx context.Context, q *dbq.Queries, serverRoot string) error {
	defer php.LockIni()()
	_, err := writeManagedIni(ctx, q, serverRoot, php.GlobalScope)
	return err
}

func (s *Server) writePHPIni(ctx context.Context, scope string, restart bool) error {
	defer php.LockIni()()
	written, err := writeManagedIni(ctx, s.queries, s.serverRoot, scope)
	errs := []error{err}
	if restart {
		for _, ver := range written {
			if !s.supervisor.IsRunning(php.FPMServiceID(ver)) {
				continue
			}
			if err := s.supervisor.Restart(s.phpFPMServiceDef(ver)); err != nil {
				errs = append(errs, fmt.Errorf("restart PHP %s FPM: %w", ver, err))
			}
		}
	}
	return errors.Join(errs...)
}

// writeManagedIni rewrites the managed block of every installed version that
// scope covers and returns the versions it wrote. Callers hold php.LockIni.
func writeManagedIni(ctx context.Context, q *dbq.Queries, serverRoot, scope string) ([]string, error) {
	ds, err := listPHPIniDirectives(ctx, q)
	if err != nil {
		return nil, err
	}
	versions, err := php.InstalledVersions(serverRoot)
	if err != nil {
		return nil, err
	}
	var written []string
	var errs []error
	for _, v := range versions {
		if scope != php.GlobalScope && v.Version != scope {
			continue
		}
		if err := php.WriteManagedBlock(php.PHPIniPath(v.Version, serverRoot), php.EffectiveDirectives(ds, v.Version)); err != nil {
			errs = append(errs, fmt.Errorf("PHP %s: %w", v.Version, err))
			continue
		}
		written = append(written, v.Version)
	}
	return written, errors.Join(errs...)
}

// phpSettingsFor builds the settings view for scope. The base values come from
// php.ini outside the managed block (the newest version stands in for
// GlobalScope); directives for the scope are laid over them.
func (s *Server) phpSettingsFor(ctx context.Context, scope string) (phpSettingsView, error) {
	view := phpSettingsView{Scope: scope, Overrides: []string{}}
	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		return view, err
	}
	if len(versions) == 0 {
		view.GlobalSettings = defaultPHPSettings
		return view, nil
	}
	baseVer := scope
	if scope == php.GlobalScope {
		baseVer = versions[0].Version
	}
	base, err := php.BaseSettings(baseVer, s.serverRoot)
	if err != nil {
		return view, err
	}
	ds, err := listPHPIniDirectives(ctx, s.queries)
	if err != nil {
		return view, err
	}
	base.Overlay(php.EffectiveDirectives(ds, scope))
	view.GlobalSettings = base
	for _, d := range ds {
		if d.Scope == scope && slices.Contains(php.SettingKeys, d.Key) {
			view.Overrides = append(view.Overrides, d.Key)
		}
	}
	return view, nil
}

func (s *Server) handleGetPHPSettings(w http.ResponseWriter, r *http.Request) {
	scope, code, err := s.resolvePHPScope(r.URL.Query().Get("version"))
	if err != nil {
		writeError(w, err.Error(), code)
		return
	}
	view, err := s.phpSettingsFor(r.Context(), scope)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

// handleSetPHPSettings stores each non-empty field as a directive in the
// scope given by ?version= (all versions when absent).
func (s *Server) handleSetPHPSettings(w http.ResponseWriter, r *http.Request) {
	scope, code, err := s.resolvePHPScope(r.URL.Query().Get("version"))
	if err != nil {
		writeError(w, err.Error(), code)
		return
	}
	var settings php.GlobalSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	kv := settings.Values()
	for k, v := range kv {
		if err := php.ValidateDirective(k, v); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := s.setPHPIniDirectives(r.Context(), scope, kv); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	view, err := s.phpSettingsFor(r.Context(), scope)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

func (s *Server) handleListPHPIni(w http.ResponseWriter, r *http.Request) {
	ds, err := listPHPIniDirectives(r.Context(), s.queries)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if scope := r.URL.Query().Get("scope"); scope != "" {
		if err := php.ValidateScope(scope); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		ds = slices.DeleteFunc(ds, func(d php.Directive) bool { return d.Scope != scope })
	}
	writeJSON(w, ds)
}

func (s *Server) handleSetPHPIni(w http.ResponseWriter, r *http.Request) {
	scope, code, err := s.resolvePHPScope(r.PathValue("scope"))
	if err != nil {
		writeError(w, err.Error(), code)
		return
	}
	key := r.PathValue("key")
	var body struct {
		Value *string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Value == nil {
		writeError(w, `body must be {"value": "..."}`, http.StatusBadRequest)
		return
	}
	if err := php.ValidateDirective(key, *body.Value); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.setPHPIniDirectives(r.Context(), scope, map[string]string{key: *body.Value}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, php.Directive{Scope: scope, Key: key, Value: *body.Value})
}

// handleUnsetPHPIni does not require the version to be installed, so
// directives left behind by an uninstalled version can be removed.
func (s *Server) handleUnsetPHPIni(w http.ResponseWriter, r *http.Request) {
	scope, key := r.PathValue("scope"), r.PathValue("key")
	if err := php.ValidateScope(scope); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	found, err := s.unsetPHPIniDirective(r.Context(), scope, key)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		writeError(w, fmt.Sprintf("%s is not set for %s", key, scope), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
