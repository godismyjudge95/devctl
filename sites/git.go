package sites

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectType represents the detected type of a PHP project.
type ProjectType string

const (
	ProjectTypeLaravel   ProjectType = "laravel"
	ProjectTypeStatamic  ProjectType = "statamic"
	ProjectTypeWordPress ProjectType = "wordpress"
	ProjectTypeGeneric   ProjectType = "generic"
)

// WorktreeSetupConfig defines which paths to symlink vs copy when creating a worktree.
type WorktreeSetupConfig struct {
	Symlinks []string `json:"symlinks"`
	Copies   []string `json:"copies"`
}

// Branch represents a git branch.
type Branch struct {
	Name      string `json:"name"`
	IsRemote  bool   `json:"is_remote"`
	IsCurrent bool   `json:"is_current"`
}

// GitWorktreeInfo holds information about a git worktree from `git worktree list`.
type GitWorktreeInfo struct {
	Path   string
	HEAD   string
	Branch string // empty if detached
}

// IsGitRepo reports whether the given directory contains a .git entry (file or directory).
// Both the main worktree (.git dir) and linked worktrees (.git file) return true.
func IsGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// IsLinkedWorktree reports whether path is a linked git worktree (has a .git FILE, not a dir).
// Linked worktrees have a .git file containing the gitdir pointer.
func IsLinkedWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// GetGitRoot returns the absolute path of the top-level working tree for the repo at path.
// For linked worktrees this is the linked worktree's root, not the main repo.
func GetGitRoot(path string) (string, error) {
	out, err := runGit(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// GetMainWorktreePath returns the path of the main (first) worktree for the repo at path.
// Works from any worktree, including linked ones.
func GetMainWorktreePath(path string) (string, error) {
	out, err := runGit(path, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("git worktree list: %w", err)
	}
	// The first "worktree <path>" entry is always the main worktree.
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree "), nil
		}
	}
	return "", fmt.Errorf("no worktree entries found")
}

// GetCurrentBranch returns the currently checked-out branch name for the repo at path.
// Returns empty string if in detached HEAD state.
func GetCurrentBranch(path string) string {
	out, err := runGit(path, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ListBranches returns all local and remote branches for the repo at path.
func ListBranches(path string) ([]Branch, error) {
	// List local branches with current marker.
	out, err := runGit(path, "branch", "--format=%(refname:short)\t%(HEAD)")
	if err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}
	seen := map[string]bool{}
	var branches []Branch
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 2)
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		name := parts[0]
		isCurrent := len(parts) == 2 && parts[1] == "*"
		seen[name] = true
		branches = append(branches, Branch{Name: name, IsRemote: false, IsCurrent: isCurrent})
	}

	// List remote branches.
	out, err = runGit(path, "branch", "-r", "--format=%(refname:short)")
	if err == nil {
		scanner = bufio.NewScanner(strings.NewReader(out))
		for scanner.Scan() {
			name := strings.TrimSpace(scanner.Text())
			if name == "" {
				continue
			}
			// Strip common remote prefix (e.g. "origin/HEAD -> origin/main").
			if strings.Contains(name, " -> ") {
				continue
			}
			// Strip "origin/" prefix for display, keep full name for checkout.
			if !seen[name] {
				branches = append(branches, Branch{Name: name, IsRemote: true, IsCurrent: false})
			}
		}
	}

	return branches, nil
}

// ListGitWorktrees returns the list of worktrees for the repo at path.
func ListGitWorktrees(path string) ([]GitWorktreeInfo, error) {
	out, err := runGit(path, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	var result []GitWorktreeInfo
	var current GitWorktreeInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current.Path != "" {
				result = append(result, current)
			}
			current = GitWorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			// Strip "refs/heads/" prefix.
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if current.Path != "" {
		result = append(result, current)
	}
	return result, nil
}

// DetectProjectType inspects the directory for known project markers.
func DetectProjectType(path string) ProjectType {
	// Statamic: has a "please" binary or statamic directory in vendor.
	if fileExists(filepath.Join(path, "please")) {
		return ProjectTypeStatamic
	}
	if fileExists(filepath.Join(path, "vendor", "statamic")) {
		return ProjectTypeStatamic
	}
	// Laravel: has an artisan file.
	if fileExists(filepath.Join(path, "artisan")) {
		return ProjectTypeLaravel
	}
	// WordPress: has wp-config or wp-config-sample.
	if fileExists(filepath.Join(path, "wp-config.php")) || fileExists(filepath.Join(path, "wp-config-sample.php")) {
		return ProjectTypeWordPress
	}
	return ProjectTypeGeneric
}

// DefaultWorktreeConfig returns sensible defaults for the given project type.
func DefaultWorktreeConfig(pt ProjectType) WorktreeSetupConfig {
	switch pt {
	case ProjectTypeLaravel:
		return WorktreeSetupConfig{
			Symlinks: []string{"vendor", "node_modules"},
			Copies:   []string{".env"},
		}
	case ProjectTypeStatamic:
		return WorktreeSetupConfig{
			Symlinks: []string{"vendor", "node_modules"},
			Copies:   []string{".env"},
		}
	case ProjectTypeWordPress:
		return WorktreeSetupConfig{
			Symlinks: []string{},
			Copies:   []string{".env", "wp-config.php"},
		}
	default:
		return WorktreeSetupConfig{
			Symlinks: []string{"vendor", "node_modules"},
			Copies:   []string{},
		}
	}
}

// SlugifyBranch converts a branch name to a URL/directory-safe slug.
// e.g. "feature/my-thing" → "feature-my-thing"
func SlugifyBranch(branch string) string {
	slug := strings.ToLower(branch)
	slug = strings.ReplaceAll(slug, "/", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	// Remove "origin-" prefix from remote branches.
	slug = strings.TrimPrefix(slug, "origin-")
	return slug
}

// CreateGitWorktree creates a new git worktree at dest from mainRepoPath on the given branch.
// If createBranch is true, a new local branch is created.
// After creating the worktree, symlinks and copies from config are set up.
func CreateGitWorktree(mainRepoPath, dest, branch string, createBranch bool, config WorktreeSetupConfig) error {
	var args []string
	if createBranch {
		args = []string{"worktree", "add", "-b", branch, dest}
	} else {
		args = []string{"worktree", "add", dest, branch}
	}
	if _, err := runGit(mainRepoPath, args...); err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}

	// Set up symlinks.
	for _, rel := range config.Symlinks {
		src := filepath.Join(mainRepoPath, rel)
		dst := filepath.Join(dest, rel)
		if !fileExists(src) {
			continue // skip if source doesn't exist in main repo
		}
		// Remove destination if it exists (e.g. git may have created an empty dir).
		_ = os.RemoveAll(dst)
		if err := os.Symlink(src, dst); err != nil {
			fmt.Printf("worktree: symlink %s → %s: %v\n", src, dst, err)
		}
	}

	// Copy files.
	for _, rel := range config.Copies {
		src := filepath.Join(mainRepoPath, rel)
		dst := filepath.Join(dest, rel)
		if !fileExists(src) {
			continue // skip if source doesn't exist
		}
		if err := copyFile(src, dst); err != nil {
			fmt.Printf("worktree: copy %s → %s: %v\n", src, dst, err)
		}
	}

	return nil
}

// RemoveGitWorktree removes a linked worktree from the git repo and deletes its directory.
func RemoveGitWorktree(mainRepoPath, worktreePath string) error {
	// Try graceful remove first, then force.
	if _, err := runGit(mainRepoPath, "worktree", "remove", worktreePath); err != nil {
		if _, err2 := runGit(mainRepoPath, "worktree", "remove", "--force", worktreePath); err2 != nil {
			// If the directory no longer exists git will still prune it.
			fmt.Printf("worktree: remove warning: %v\n", err2)
		}
	}
	// Prune stale worktree entries.
	_, _ = runGit(mainRepoPath, "worktree", "prune")
	// Remove directory if it still exists (e.g. if git remove failed).
	if _, err := os.Stat(worktreePath); err == nil {
		if err := os.RemoveAll(worktreePath); err != nil {
			return fmt.Errorf("remove worktree dir: %w", err)
		}
	}
	return nil
}

// --- helpers ---

func runGit(dir string, args ...string) (string, error) {
	// Prepend -c safe.directory=* so git doesn't refuse to operate on
	// directories owned by a different user (devctl runs as root, sites are
	// typically owned by the developer).
	fullArgs := append([]string{"-c", "safe.directory=*"}, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
