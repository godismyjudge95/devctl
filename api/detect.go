package api

import (
	"net/http"
	"os"
	"path/filepath"
)

// DetectFramework returns the framework name for the given root path.
// Signals:
//   - "artisan" file present + "content/" directory → "statamic"
//   - "artisan" file present → "laravel"
//   - "wp-config.php" or "wp-login.php" present → "wordpress"
//   - otherwise → ""
func DetectFramework(rootPath string) string {
	if fileExists(filepath.Join(rootPath, "artisan")) {
		if dirExists(filepath.Join(rootPath, "content")) {
			return "statamic"
		}
		return "laravel"
	}
	if fileExists(filepath.Join(rootPath, "wp-config.php")) || fileExists(filepath.Join(rootPath, "wp-login.php")) {
		return "wordpress"
	}
	return ""
}

// DetectPublicDir returns the public subdirectory for the given root path.
// Laravel/Statamic use "public"; WordPress and unknown projects use "".
func DetectPublicDir(rootPath string) string {
	fw := DetectFramework(rootPath)
	switch fw {
	case "laravel", "statamic":
		return "public"
	default:
		return ""
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// handleDetectSite handles GET /api/sites/detect?root_path=...
// It returns the auto-detected public_dir and framework for the given root path.
func (s *Server) handleDetectSite(w http.ResponseWriter, r *http.Request) {
	rootPath := r.URL.Query().Get("root_path")
	if rootPath == "" {
		writeError(w, "root_path query parameter is required", http.StatusBadRequest)
		return
	}

	framework := DetectFramework(rootPath)
	publicDir := DetectPublicDir(rootPath)

	writeJSON(w, map[string]string{
		"public_dir": publicDir,
		"framework":  framework,
	})
}
