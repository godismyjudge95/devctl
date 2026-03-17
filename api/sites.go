package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/sites"
)

type siteRequest struct {
	Domain     string   `json:"domain"`
	RootPath   string   `json:"root_path"`
	PHPVersion string   `json:"php_version"`
	Aliases    []string `json:"aliases"`
	SPXEnabled bool     `json:"spx_enabled"`
	HTTPS      bool     `json:"https"`
	PublicDir  string   `json:"public_dir"`
}

func (s *Server) handleGetSites(w http.ResponseWriter, r *http.Request) {
	all, err := s.queries.GetUserSites(context.Background())
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, all)
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req siteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
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

	site, err := s.siteManager.Create(r.Context(), sites.CreateSiteInput{
		Domain:       req.Domain,
		RootPath:     req.RootPath,
		PHPVersion:   req.PHPVersion,
		Aliases:      req.Aliases,
		HTTPS:        req.HTTPS,
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
	writeJSON(w, site)
}

func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	writeJSON(w, site)
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req siteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Load current site to preserve settings (settings are not user-editable here).
	existing, err := s.queries.GetSite(context.Background(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}

	aliases, _ := json.Marshal(req.Aliases)
	spx := int64(0)
	if req.SPXEnabled {
		spx = 1
	}
	httpsVal := int64(1)
	if !req.HTTPS {
		httpsVal = 0
	}

	// Re-inspect if root_path changed; otherwise preserve existing git/framework data.
	isGitRepo := existing.IsGitRepo
	gitRemoteURL := existing.GitRemoteURL
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

	site, err := s.queries.UpdateSite(context.Background(), dbq.UpdateSiteParams{
		Domain:       req.Domain,
		RootPath:     req.RootPath,
		PhpVersion:   req.PHPVersion,
		Aliases:      string(aliases),
		SpxEnabled:   spx,
		Https:        httpsVal,
		Settings:     existing.Settings,
		PublicDir:    req.PublicDir,
		IsGitRepo:    isGitRepo,
		GitRemoteURL: gitRemoteURL,
		Framework:    framework,
		ID:           id,
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync updated site to Caddy (preserving site type from settings).
	var aliasList []string
	_ = json.Unmarshal([]byte(site.Aliases), &aliasList)
	var settings map[string]string
	_ = json.Unmarshal([]byte(site.Settings), &settings)
	hosts := append([]string{site.Domain}, aliasList...)
	_ = s.caddy.UpsertVhost(sites.VhostConfig{
		ID:         "vhost-" + site.ID,
		Hosts:      hosts,
		RootPath:   site.RootPath,
		PublicDir:  site.PublicDir,
		PHPVersion: site.PhpVersion,
		HTTPS:      site.Https == 1,
		SiteType:   settings["site_type"],
		WSUpstream: settings["ws_upstream"],
	})

	// Ensure the PHP-FPM process for the selected version is running.
	if site.PhpVersion != "" && settings["site_type"] != "ws" {
		fpmID := php.FPMServiceID(site.PhpVersion)
		if !s.supervisor.IsRunning(fpmID) {
			def := s.phpFPMServiceDef(site.PhpVersion)
			_ = s.supervisor.Start(def)
		}
	}

	writeJSON(w, site)
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
		info := InspectSitePath(site.RootPath)
		isGitRepo := int64(0)
		if info.IsGitRepo {
			isGitRepo = 1
		}
		if err := s.queries.UpdateSiteGitInfo(r.Context(), dbq.UpdateSiteGitInfoParams{
			IsGitRepo:    isGitRepo,
			GitRemoteURL: info.GitRemoteURL,
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
	if err := s.queries.SetSiteSPX(context.Background(), dbq.SetSiteSPXParams{SpxEnabled: 1, ID: id}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleSPXDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.queries.SetSiteSPX(context.Background(), dbq.SetSiteSPXParams{SpxEnabled: 0, ID: id}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
