package sites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SiteInspection holds all auto-detected information about a project directory.
type SiteInspection struct {
	Framework    string
	PublicDir    string
	IsGitRepo    bool
	GitRemoteURL string
}

// InspectPath detects the framework, public directory, and git state for a
// given root path. Detection rules (first match wins):
//
//  1. vendor/statamic/ directory present                    → statamic, public_dir=public
//  2. "please" file present (Statamic CLI)                  → statamic, public_dir=public
//  3. composer.json contains "statamic/cms"                 → statamic, public_dir=public
//  4. artisan file present                                   → laravel,   public_dir=public
//  5. composer.json contains "illuminate/foundation"        → laravel,   public_dir=public
//  6. composer.json contains "craftcms/cms" or "craft" file → craft,     public_dir=web
//  7. drupal/core, core/lib/Drupal.php, or web/core/...     → drupal,    public_dir=web (composer) or ""
//  8. wp-config.php, web/wp-config.php, or wp-config-sample → wordpress, public_dir=web (Bedrock) or ""
//  9. index.php contains "wp-blog-header" or "wp-load.php"  → wordpress, public_dir=""
//
// 10. composer.json contains "symfony/framework-bundle"     → symfony,   public_dir=public
// 11. otherwise                                              → "",        DetectPublicDir fallback
func InspectPath(rootPath string) SiteInspection {
	framework := DetectFramework(rootPath)
	publicDir := DetectPublicDir(rootPath)

	isGit := IsGitRepo(rootPath)
	remoteURL := ""
	if isGit {
		remoteURL = GetRemoteURL(rootPath)
	}

	return SiteInspection{
		Framework:    framework,
		PublicDir:    publicDir,
		IsGitRepo:    isGit,
		GitRemoteURL: remoteURL,
	}
}

// DetectFramework returns the framework name for the given root path.
// It consolidates all detection signals into a single authoritative function.
func DetectFramework(rootPath string) string {
	// 1. vendor/statamic/ directory → Statamic
	if inspectDirExists(filepath.Join(rootPath, "vendor", "statamic")) {
		return "statamic"
	}
	// 2. "please" binary (Statamic CLI) → Statamic
	if inspectFileExists(filepath.Join(rootPath, "please")) {
		return "statamic"
	}
	// 3. composer.json contains statamic/cms → Statamic
	if composerRequires(rootPath, "statamic/cms") {
		return "statamic"
	}
	// 4. artisan file → Laravel
	if inspectFileExists(filepath.Join(rootPath, "artisan")) {
		return "laravel"
	}
	// 5. composer.json contains illuminate/foundation → Laravel
	if composerRequires(rootPath, "illuminate/foundation") {
		return "laravel"
	}
	// 6. Craft CMS
	if composerRequires(rootPath, "craftcms/cms") || inspectFileExists(filepath.Join(rootPath, "craft")) {
		return "craft"
	}
	// 7. Drupal (composer layout or unpacked tarball)
	if composerRequires(rootPath, "drupal/core") ||
		composerRequires(rootPath, "drupal/core-recommended") ||
		inspectFileExists(filepath.Join(rootPath, "core", "lib", "Drupal.php")) ||
		inspectFileExists(filepath.Join(rootPath, "web", "core", "lib", "Drupal.php")) {
		return "drupal"
	}
	// 8. WordPress (classic, Bedrock, or sample config)
	if inspectFileExists(filepath.Join(rootPath, "wp-config.php")) ||
		inspectFileExists(filepath.Join(rootPath, "wp-config-sample.php")) ||
		inspectFileExists(filepath.Join(rootPath, "web", "wp-config.php")) {
		return "wordpress"
	}
	// 9. index.php contains WordPress bootstrap calls → WordPress
	if indexPHPContains(rootPath, "wp-blog-header") ||
		indexPHPContains(rootPath, "wp-load.php") {
		return "wordpress"
	}
	// 10. Symfony
	if composerRequires(rootPath, "symfony/framework-bundle") ||
		composerRequires(rootPath, "symfony/runtime") {
		return "symfony"
	}
	return ""
}

// DetectPublicDir returns the public subdirectory for the given root path.
func DetectPublicDir(rootPath string) string {
	switch DetectFramework(rootPath) {
	case "laravel", "statamic", "symfony":
		return "public"
	case "craft":
		if inspectDirExists(filepath.Join(rootPath, "web")) {
			return "web"
		}
		if inspectDirExists(filepath.Join(rootPath, "public")) {
			return "public"
		}
		return "web"
	case "drupal":
		if inspectFileExists(filepath.Join(rootPath, "web", "index.php")) {
			return "web"
		}
		return ""
	case "wordpress":
		if inspectFileExists(filepath.Join(rootPath, "web", "index.php")) ||
			inspectFileExists(filepath.Join(rootPath, "web", "wp-config.php")) {
			return "web"
		}
		return ""
	default:
		if inspectFileExists(filepath.Join(rootPath, "public", "index.php")) {
			return "public"
		}
		if inspectFileExists(filepath.Join(rootPath, "web", "index.php")) {
			return "web"
		}
		return ""
	}
}

// --- helpers (unexported, inspection-specific) ---

func inspectFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func inspectDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// composerRequires reports whether composer.json in rootPath lists packageName
// as a dependency (in "require" or "require-dev").
func composerRequires(rootPath, packageName string) bool {
	data, err := os.ReadFile(filepath.Join(rootPath, "composer.json"))
	if err != nil {
		return false
	}
	var composer struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return false
	}
	if _, ok := composer.Require[packageName]; ok {
		return true
	}
	if _, ok := composer.RequireDev[packageName]; ok {
		return true
	}
	return false
}

// indexPHPContains reports whether index.php in rootPath contains the given substring.
func indexPHPContains(rootPath, substr string) bool {
	data, err := os.ReadFile(filepath.Join(rootPath, "index.php"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), substr)
}
