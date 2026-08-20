package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// envRewriteFiles are paths (relative to the worktree root) that commonly
// contain the parent site's hostname and should be rewritten after a copy.
var envRewriteFiles = []string{
	".env",
	".env.local",
	".env.development",
	".env.development.local",
	"config/.env",
	"wp-config.php",
	"web/wp-config.php",
	"web/sites/default/settings.local.php",
	"sites/default/settings.local.php",
}

// RewriteWorktreeEnvFiles rewrites the parent hostname to the worktree
// hostname in copied env/config files, then ensures APP_URL (when present)
// points at the worktree vhost.
func RewriteWorktreeEnvFiles(worktreePath, parentDomain, worktreeDomain string, https bool) {
	if worktreeDomain == "" {
		return
	}
	scheme := "http"
	if https {
		scheme = "https"
	}

	for _, rel := range envRewriteFiles {
		path := filepath.Join(worktreePath, rel)
		if !fileExists(path) {
			continue
		}
		if err := rewriteDomainsInFile(path, parentDomain, worktreeDomain, scheme); err != nil {
			fmt.Printf("worktree: rewrite %s: %v\n", rel, err)
		}
	}

	envPath := filepath.Join(worktreePath, ".env")
	if fileExists(envPath) {
		if val, ok := dotenvValue(envPath, "APP_URL"); ok && !strings.Contains(val, worktreeDomain) {
			_ = setDotenvKey(envPath, "APP_URL", scheme+"://"+worktreeDomain)
		}
		if val, ok := dotenvValue(envPath, "SESSION_DOMAIN"); ok && val != "" && !strings.Contains(val, worktreeDomain) && (strings.Contains(val, parentDomain) || val == "localhost" || val == "127.0.0.1") {
			_ = setDotenvKey(envPath, "SESSION_DOMAIN", worktreeDomain)
		}
	}
}

func rewriteDomainsInFile(path, parentDomain, worktreeDomain, scheme string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	orig := s
	if parentDomain != "" && parentDomain != worktreeDomain {
		s = strings.ReplaceAll(s, "https://"+parentDomain, scheme+"://"+worktreeDomain)
		s = strings.ReplaceAll(s, "http://"+parentDomain, scheme+"://"+worktreeDomain)
		s = strings.ReplaceAll(s, parentDomain, worktreeDomain)
	}
	// Normalise scheme on any worktree-domain URLs (covers localhost APP_URL
	// that was later rewritten, and mixed http/https leftovers).
	s = strings.ReplaceAll(s, "http://"+worktreeDomain, scheme+"://"+worktreeDomain)
	s = strings.ReplaceAll(s, "https://"+worktreeDomain, scheme+"://"+worktreeDomain)
	if s == orig {
		return nil
	}
	info, err := os.Stat(path)
	perm := os.FileMode(0644)
	if err == nil {
		perm = info.Mode().Perm()
	}
	return os.WriteFile(path, []byte(s), perm)
}

func dotenvValue(path, key string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		val := strings.TrimPrefix(trimmed, prefix)
		val = strings.Trim(val, `"'`)
		return val, true
	}
	return "", false
}

func setDotenvKey(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, _ := os.Stat(path)
	perm := os.FileMode(0644)
	if info != nil {
		perm = info.Mode().Perm()
	}
	lines := strings.Split(string(data), "\n")
	prefix := key + "="
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}
	if !found {
		if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
			lines = []string{key + "=" + value}
		} else {
			if lines[len(lines)-1] == "" {
				lines[len(lines)-1] = key + "=" + value
				lines = append(lines, "")
			} else {
				lines = append(lines, key+"="+value)
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), perm)
}
