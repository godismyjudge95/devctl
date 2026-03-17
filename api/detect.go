package api

import (
	"net/http"

	"github.com/danielgormly/devctl/sites"
)

// InspectSitePath is the API-layer wrapper around sites.InspectPath.
// It returns the auto-detected framework, public directory, git repo status,
// and git remote URL for the given root path.
func InspectSitePath(rootPath string) sites.SiteInspection {
	return sites.InspectPath(rootPath)
}

// DetectFramework is kept for backward compatibility.
func DetectFramework(rootPath string) string {
	return sites.DetectFramework(rootPath)
}

// DetectPublicDir is kept for backward compatibility.
func DetectPublicDir(rootPath string) string {
	return sites.DetectPublicDir(rootPath)
}

// handleDetectSite handles GET /api/sites/detect?root_path=...
// It returns the auto-detected public_dir and framework for the given root path.
func (s *Server) handleDetectSite(w http.ResponseWriter, r *http.Request) {
	rootPath := r.URL.Query().Get("root_path")
	if rootPath == "" {
		writeError(w, "root_path query parameter is required", http.StatusBadRequest)
		return
	}

	info := InspectSitePath(rootPath)
	writeJSON(w, map[string]string{
		"public_dir": info.PublicDir,
		"framework":  info.Framework,
	})
}
