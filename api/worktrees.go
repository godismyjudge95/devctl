package api

import (
	"encoding/json"
	"net/http"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/sites"
)

// handleGetSiteBranches lists the git branches available in the site's repository.
// GET /api/sites/{id}/branches
func (s *Server) handleGetSiteBranches(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.queries.GetSite(r.Context(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}

	branches, err := sites.ListBranches(site.RootPath)
	if err != nil {
		writeError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, branches)
}

// handleGetWorktreeConfig returns the worktree setup config for a site.
// If the site has no saved config, sensible defaults are derived from the project type.
// GET /api/sites/{id}/worktree-config
func (s *Server) handleGetWorktreeConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.queries.GetSite(r.Context(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}

	config := worktreeConfigFromSite(site)
	writeJSON(w, config)
}

// handlePutWorktreeConfig saves the worktree setup config for a site.
// PUT /api/sites/{id}/worktree-config
func (s *Server) handlePutWorktreeConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	site, err := s.queries.GetSite(r.Context(), id)
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}

	var req sites.WorktreeSetupConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Merge into existing settings.
	var settingsMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(site.Settings), &settingsMap); err != nil {
		settingsMap = make(map[string]json.RawMessage)
	}

	symlinksJSON, _ := json.Marshal(req.Symlinks)
	copiesJSON, _ := json.Marshal(req.Copies)
	settingsMap["worktree_symlinks"] = symlinksJSON
	settingsMap["worktree_copies"] = copiesJSON

	merged, _ := json.Marshal(settingsMap)
	if err := s.queries.UpdateSiteSettings(r.Context(), dbq.UpdateSiteSettingsParams{
		Settings: string(merged),
		ID:       id,
	}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, req)
}

// handleGetSiteWorktrees returns all worktree sites linked to the given parent site.
// GET /api/sites/{id}/worktrees
func (s *Server) handleGetSiteWorktrees(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	worktrees, err := s.queries.GetWorktreesBySite(r.Context(), &id)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if worktrees == nil {
		worktrees = []dbq.Site{}
	}
	writeJSON(w, worktrees)
}

// handleCreateWorktree creates a new git worktree for the given parent site.
// POST /api/sites/{id}/worktrees
func (s *Server) handleCreateWorktree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		Branch       string   `json:"branch"`
		CreateBranch bool     `json:"create_branch"`
		Symlinks     []string `json:"symlinks"`
		Copies       []string `json:"copies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Branch == "" {
		writeError(w, "branch is required", http.StatusBadRequest)
		return
	}

	config := sites.WorktreeSetupConfig{
		Symlinks: req.Symlinks,
		Copies:   req.Copies,
	}

	site, err := s.siteManager.CreateWorktree(r.Context(), id, req.Branch, req.CreateBranch, config)
	if err != nil {
		writeError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, site)
}

// handleRemoveWorktree removes a git worktree site (git + site record).
// DELETE /api/sites/{id}/worktrees/{worktreeId}
func (s *Server) handleRemoveWorktree(w http.ResponseWriter, r *http.Request) {
	worktreeID := r.PathValue("worktreeId")
	if err := s.siteManager.RemoveWorktree(r.Context(), worktreeID); err != nil {
		writeError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// worktreeConfigFromSite extracts the WorktreeSetupConfig from the site's settings JSON.
// Falls back to project-type defaults if not configured.
func worktreeConfigFromSite(site dbq.Site) sites.WorktreeSetupConfig {
	var settingsMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(site.Settings), &settingsMap); err == nil {
		var symlinks []string
		var copies []string
		symlinksSet := false
		copiesSet := false

		if raw, ok := settingsMap["worktree_symlinks"]; ok {
			if err := json.Unmarshal(raw, &symlinks); err == nil {
				symlinksSet = true
			}
		}
		if raw, ok := settingsMap["worktree_copies"]; ok {
			if err := json.Unmarshal(raw, &copies); err == nil {
				copiesSet = true
			}
		}

		if symlinksSet || copiesSet {
			return sites.WorktreeSetupConfig{Symlinks: symlinks, Copies: copies}
		}
	}

	// No saved config — derive defaults from project type.
	pt := sites.DetectProjectType(site.RootPath)
	return sites.DefaultWorktreeConfig(pt)
}
