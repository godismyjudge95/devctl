package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgormly/devctl/newsite"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/sites"
)

// Laravel DB_CONNECTION for each devctl database engine.
var laravelConnection = map[string]string{"mysql": "mysql", "postgres": "pgsql"}

const newSiteTimeout = 30 * time.Minute

func (s *Server) handleGetSiteTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"templates": newsite.Templates})
}

// handleNewSite creates a project from a template and streams progress as SSE.
// Invalid input fails before the stream starts (400, or 409 for a taken name).
// Events:
//
//	output — a chunk of command output (JSON string)
//	step   — {"index":1,"total":4,"label":"..."} before each step
//	done   — {"site":{...},"url":"https://name.test"}
//	error  — {"error":"..."}; completed steps were undone
func (s *Server) handleNewSite(w http.ResponseWriter, r *http.Request) {
	var req newsite.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	p, code, err := s.prepareNewSite(r.Context(), req)
	if err != nil {
		writeError(w, err.Error(), code)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	rt := newsite.Runtime{
		Host: newSiteHost{s},
		Exec: newsite.UserExecutor{User: s.siteUser, Home: s.siteHome, BinDir: paths.BinDir(s.serverRoot)},
		Out:  &sseLineWriter{w: w, flusher: flusher, event: "output"},
		OnStep: func(index, total int, label string) {
			sendSSE(w, flusher, "step", map[string]any{"index": index, "total": total, "label": label})
		},
	}
	// A closed tab or a Ctrl+C must not undo a long install that is nearly done.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), newSiteTimeout)
	defer cancel()
	if err := newsite.Run(ctx, p, rt); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}
	site, err := s.queries.GetSiteByDomain(ctx, p.Domain)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": "site created but not found: " + err.Error()})
		return
	}
	sendSSE(w, flusher, "done", map[string]any{"site": site, "url": "https://" + p.Domain})
}

// prepareNewSite validates req and resolves what the steps need. It returns
// the HTTP status for an error.
func (s *Server) prepareNewSite(ctx context.Context, req newsite.Request) (*newsite.Project, int, error) {
	p, err := newsite.Parse(req, s.sitesRoot(ctx))
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	if _, err := os.Lstat(p.Dir); err == nil {
		return nil, http.StatusConflict, fmt.Errorf("%s already exists", p.Dir)
	}
	if _, err := s.queries.GetSiteByDomain(ctx, p.Domain); err == nil {
		return nil, http.StatusConflict, fmt.Errorf("site %s already exists", p.Domain)
	}

	phpBin, err := s.newSitePHP(p.PHPVersion)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	p.Tools = map[string]string{newsite.ToolPHP: phpBin}
	for _, tool := range p.Template.Tools {
		path := s.newSiteToolPath(ctx, tool)
		if _, err := os.Stat(path); err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("%s not found at %s. devctl installs it with PHP. Reinstall a PHP version", tool, path)
		}
		p.Tools[tool] = path
	}

	if db := p.DB; db != nil {
		eng, err := s.openEngine(db.Engine)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("database %s: %w", db.Engine, err)
		}
		existing, err := eng.ListDatabases()
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("list %s databases: %w", db.Engine, err)
		}
		for _, d := range existing {
			if d.Name == db.Name {
				return nil, http.StatusConflict, fmt.Errorf("%s database %s already exists", db.Engine, db.Name)
			}
		}
		cfg := s.engineConfig(db.Engine)
		db.Server = newsite.DBServer{
			Connection: laravelConnection[db.Engine],
			Host:       cfg.Host, Port: cfg.Port, Username: cfg.User, Password: cfg.Password,
		}
	}

	for _, id := range []string{"mailpit", "redis"} {
		if def, ok := s.registry.Get(id); ok && def.CredentialsFile != "" {
			p.ServiceEnv = append(p.ServiceEnv, newsite.EnvVarsFromMap(readKVFile(def.CredentialsFile))...)
		}
	}
	return p, 0, nil
}

// sitesRoot is the directory the sites watcher scans.
func (s *Server) sitesRoot(ctx context.Context) string {
	if v, err := s.queries.GetSetting(ctx, "sites_watch_dir"); err == nil {
		if dir := paths.ExpandHome(v); dir != "" {
			return dir
		}
	}
	return filepath.Dir(s.serverRoot)
}

// newSitePHP returns the PHP CLI wrapper for version, or the default wrapper
// when version is empty.
func (s *Server) newSitePHP(version string) (string, error) {
	installed, err := php.InstalledVersions(s.serverRoot)
	if err != nil || len(installed) == 0 {
		return "", fmt.Errorf("no PHP version is installed. Install one on the Services page first")
	}
	name := "php"
	if version != "" {
		found := false
		for _, v := range installed {
			found = found || v.Version == version
		}
		if !found {
			return "", fmt.Errorf("PHP %s is not installed", version)
		}
		name += version
	}
	path := filepath.Join(paths.BinDir(s.serverRoot), name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("PHP CLI wrapper not found at %s", path)
	}
	return path, nil
}

// newSiteToolPath is wp or composer in the devctl bin directory, or an
// installer in the site user's Composer global bin directory.
func (s *Server) newSiteToolPath(ctx context.Context, tool string) string {
	binDir := paths.BinDir(s.serverRoot)
	if tool == newsite.ToolWP || tool == newsite.ToolComposer {
		return filepath.Join(binDir, tool)
	}
	composerBin := filepath.Join(binDir, "composer")
	return filepath.Join(php.ComposerGlobalBinDir(ctx, composerBin, s.siteUser, s.siteHome), tool)
}

// newSiteHost connects the wizard to the site manager and the database engines.
type newSiteHost struct{ s *Server }

func (h newSiteHost) Register(ctx context.Context, p *newsite.Project) error {
	_, err := h.s.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:     p.Domain,
		RootPath:   p.Dir,
		PHPVersion: p.PHPVersion,
		PublicDir:  p.Template.PublicDir,
		HTTPS:      true,
		Framework:  p.Template.Framework,
	})
	return err
}

func (h newSiteHost) Unregister(ctx context.Context, p *newsite.Project) error {
	return h.s.siteManager.Delete(ctx, sites.DomainToID(p.Domain))
}

func (h newSiteHost) CreateDatabase(_ context.Context, engine, name string) error {
	eng, err := h.s.openEngine(engine)
	if err != nil {
		return err
	}
	return eng.CreateDatabase(name)
}

func (h newSiteHost) DropDatabase(_ context.Context, engine, name string) error {
	eng, err := h.s.openEngine(engine)
	if err != nil {
		return err
	}
	return eng.DropDatabase(name)
}
