package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/sites"
)

type siteRequest struct {
	Domain     string   `json:"domain"`
	RootPath   string   `json:"root_path"`
	PHPVersion string   `json:"php_version"`
	Aliases    []string `json:"aliases"`
	SPXEnabled int64    `json:"spx_enabled"`
	HTTPS      *int64   `json:"https"`
	CORS       *int64   `json:"cors"`
	PublicDir  string   `json:"public_dir"`
	// Proxy creates or updates a proxy site. Its upstream is validated while
	// the body is decoded.
	Proxy *sites.Proxy `json:"proxy"`
}

// errProxyFields is returned when a proxy site request sets a PHP site field.
const errProxyFields = "a proxy site has no root_path, php_version, or public_dir"

func (req siteRequest) hasPHPFields() bool {
	return req.RootPath != "" || req.PHPVersion != "" || req.PublicDir != ""
}

func (s *Server) handleGetSites(w http.ResponseWriter, r *http.Request) {
	all, err := s.queries.GetUserSites(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.siteViews(all))
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req siteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Proxy != nil {
		s.createProxySite(w, r, req)
		return
	}

	if req.Domain == "" || req.RootPath == "" {
		writeError(w, "domain and root_path are required", http.StatusBadRequest)
		return
	}

	// Inspect the root path: detect framework, public dir, git info.
	info := InspectSitePath(req.RootPath)

	// Caller-supplied public_dir wins; fall back to detected value.
	publicDir := req.PublicDir
	if publicDir == "" {
		publicDir = info.PublicDir
	}

	httpsVal := int64(1)
	if req.HTTPS != nil {
		httpsVal = *req.HTTPS
	}
	corsVal := int64(0)
	if req.CORS != nil {
		corsVal = *req.CORS
	}

	site, err := s.siteManager.Create(r.Context(), sites.CreateSiteInput{
		Domain:       req.Domain,
		RootPath:     req.RootPath,
		PHPVersion:   req.PHPVersion,
		Aliases:      req.Aliases,
		HTTPS:        httpsVal == 1,
		CORS:         corsVal == 1,
		PublicDir:    publicDir,
		IsGitRepo:    info.IsGitRepo,
		GitRemoteURL: info.GitRemoteURL,
		Framework:    info.Framework,
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, s.siteView(site))
}

func (s *Server) createProxySite(w http.ResponseWriter, r *http.Request, req siteRequest) {
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if err := sites.ValidateProxyDomain(domain); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.hasPHPFields() {
		writeError(w, errProxyFields, http.StatusBadRequest)
		return
	}
	if _, err := s.queries.GetSiteByDomain(r.Context(), domain); err == nil {
		writeError(w, "a site with domain "+domain+" already exists", http.StatusConflict)
		return
	}
	site, err := s.siteManager.Create(r.Context(), sites.CreateSiteInput{
		Domain:  domain,
		Aliases: req.Aliases,
		HTTPS:   req.HTTPS == nil || *req.HTTPS == 1,
		CORS:    req.CORS != nil && *req.CORS == 1,
		Proxy:   req.Proxy,
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, s.siteView(site))
}

func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	writeJSON(w, s.siteView(site))
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req siteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Load current site to preserve settings (settings are not user-editable here).
	existing, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}

	// A site keeps its type. Without a proxy in the body, a proxy site keeps
	// its upstream, so the PHP site edits of the CLI (CORS, SPX) work on it.
	settings := existing.Settings
	if sites.ProxyFromSettings(existing.Settings) != nil {
		if req.hasPHPFields() {
			writeError(w, existing.Domain+" is a proxy site: "+errProxyFields, http.StatusBadRequest)
			return
		}
		req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
		if err := sites.ValidateProxyDomain(req.Domain); err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Proxy != nil {
			if settings, err = sites.SettingsWithProxy(settings, req.Proxy); err != nil {
				writeError(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else if req.Proxy != nil {
		writeError(w, existing.Domain+" is a PHP site. Delete it first to use its domain for a proxy site", http.StatusBadRequest)
		return
	}

	aliases, _ := json.Marshal(req.Aliases)
	httpsVal := existing.Https
	if req.HTTPS != nil {
		httpsVal = *req.HTTPS
	}
	corsVal := existing.Cors
	if req.CORS != nil {
		corsVal = *req.CORS
	}
	spx := req.SPXEnabled

	// Re-inspect if root_path changed; otherwise preserve existing git/framework data.
	isGitRepo := existing.IsGitRepo
	gitRemoteURL := existing.GitRemoteUrl
	framework := existing.Framework
	if req.RootPath != existing.RootPath {
		info := InspectSitePath(req.RootPath)
		isGitRepo = 0
		if info.IsGitRepo {
			isGitRepo = 1
		}
		gitRemoteURL = info.GitRemoteURL
		framework = info.Framework
	}

	site, err := s.saveSite(r.Context(), dbq.UpdateSiteParams{
		Domain:       req.Domain,
		RootPath:     req.RootPath,
		PhpVersion:   req.PHPVersion,
		Aliases:      string(aliases),
		SpxEnabled:   spx,
		Https:        httpsVal,
		Cors:         corsVal,
		Settings:     settings,
		PublicDir:    req.PublicDir,
		IsGitRepo:    isGitRepo,
		GitRemoteUrl: gitRemoteURL,
		Framework:    framework,
		ID:           id,
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.siteView(site))
}

// saveSite stores the site row, then syncs its Caddy vhost and starts the
// PHP-FPM pool of its version. The site settings form and the project config
// both save through it.
func (s *Server) saveSite(ctx context.Context, p dbq.UpdateSiteParams) (dbq.Site, error) {
	site, err := s.queries.UpdateSite(ctx, p)
	if err != nil {
		return dbq.Site{}, err
	}

	vhost := sites.VhostFromSite(site, s.serverRoot)
	_ = s.caddy.UpsertVhost(vhost)

	// Ensure the PHP-FPM process for the selected version is running.
	if site.PhpVersion != "" && vhost.Proxy == nil {
		fpmID := php.FPMServiceID(site.PhpVersion)
		if !s.supervisor.IsRunning(fpmID) {
			def := s.phpFPMServiceDef(site.PhpVersion)
			_ = s.supervisor.Start(def)
		}
	}
	return site, nil
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.shares.Stop(id)
	if err := s.siteManager.Delete(r.Context(), id); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRefreshSiteMetadata re-inspects every site's root_path and updates
// is_git_repo, git_remote_url, and framework in the DB.
// POST /api/sites/refresh-metadata
func (s *Server) handleRefreshSiteMetadata(w http.ResponseWriter, r *http.Request) {
	all, err := s.queries.GetUserSites(r.Context())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updated := 0
	for _, site := range all {
		if site.RootPath == "" {
			continue
		}
		info := InspectSitePath(site.RootPath)
		isGitRepo := int64(0)
		if info.IsGitRepo {
			isGitRepo = 1
		}
		if err := s.queries.UpdateSiteGitInfo(r.Context(), dbq.UpdateSiteGitInfoParams{
			IsGitRepo:    isGitRepo,
			GitRemoteUrl: info.GitRemoteURL,
			Framework:    info.Framework,
			ID:           site.ID,
		}); err != nil {
			writeError(w, fmt.Sprintf("failed to update site %s: %v", site.ID, err), http.StatusInternalServerError)
			return
		}
		updated++
	}

	writeJSON(w, map[string]int{"updated": updated})
}

func (s *Server) handleSPXEnable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	site, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	if err := s.queries.SetSiteSPX(context.Background(), dbq.SetSiteSPXParams{SpxEnabled: 1, ID: id}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadPHPConfigs(site.PhpVersion)
	writeJSON(w, map[string]string{"status": "ok"})
}

// reloadPHPConfigs rewrites the PHP config of ver, which lists the sites with
// SPX on, and restarts its FPM pool so the change applies.
func (s *Server) reloadPHPConfigs(ver string) {
	if ver == "" {
		return
	}
	if err := php.WriteConfigs(ver, s.serverRoot, s.siteUser); err != nil {
		fmt.Printf("spx: rewrite configs for %s: %v\n", ver, err)
		return
	}
	_ = s.supervisor.Restart(s.phpFPMServiceDef(ver))
}

func (s *Server) handleSPXDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	site, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	if err := s.queries.SetSiteSPX(context.Background(), dbq.SetSiteSPXParams{SpxEnabled: 0, ID: id}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadPHPConfigs(site.PhpVersion)
	writeJSON(w, map[string]string{"status": "ok"})
}

// parsePage extracts ?page= and ?limit= query params with defaults.
func parsePage(r *http.Request) (limit, offset int64) {
	limit = 50
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 1 {
			offset = (n - 1) * limit
		}
	}
	return limit, offset
}

// ensure fmt is used
var _ = fmt.Sprintf
