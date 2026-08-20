package sites

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// ProjectType represents the detected type of a PHP project.
type ProjectType string

const (
	ProjectTypeLaravel   ProjectType = "laravel"
	ProjectTypeStatamic  ProjectType = "statamic"
	ProjectTypeWordPress ProjectType = "wordpress"
	ProjectTypeDrupal    ProjectType = "drupal"
	ProjectTypeCraft     ProjectType = "craft"
	ProjectTypeSymfony   ProjectType = "symfony"
	ProjectTypeGeneric   ProjectType = "generic"
)

// WorktreeSetupConfig defines which paths to symlink vs copy when creating a worktree.
// If both Symlinks and Copies are empty and NoShare is false, framework defaults apply.
type WorktreeSetupConfig struct {
	Symlinks []string `json:"symlinks"`
	Copies   []string `json:"copies"`
	NoShare  bool     `json:"no_share,omitempty"`
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

// GetRemoteURL returns the fetch URL for the "origin" remote of the repo at path.
// Returns an empty string if there is no origin remote or if path is not a git repo.
func GetRemoteURL(path string) string {
	out, err := runGit(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
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
// Delegates to DetectFramework so detection rules stay in one place.
func DetectProjectType(path string) ProjectType {
	switch DetectFramework(path) {
	case "statamic":
		return ProjectTypeStatamic
	case "laravel":
		return ProjectTypeLaravel
	case "wordpress":
		return ProjectTypeWordPress
	case "drupal":
		return ProjectTypeDrupal
	case "craft":
		return ProjectTypeCraft
	case "symfony":
		return ProjectTypeSymfony
	default:
		return ProjectTypeGeneric
	}
}

// sharedCopies are gitignored runtime dirs/files that every PHP project may want
// seeded into a new worktree. Missing sources are skipped at seed time.
var sharedCopies = []string{".env", "vendor", "node_modules"}

// DefaultWorktreeConfig returns sensible defaults for the given project type.
//
// vendor/ and node_modules/ are COPIED (or reflinked), never symlinked.
// PHP resolves __DIR__ through symlinks, so a symlinked vendor/ makes Composer's
// ClassLoader initialise against the parent checkout and silently load stale
// classes (the Lerd lesson).
func DefaultWorktreeConfig(pt ProjectType) WorktreeSetupConfig {
	copies := append([]string{}, sharedCopies...)
	var symlinks []string
	switch pt {
	case ProjectTypeWordPress:
		copies = append(copies, "wp-config.php")
		symlinks = []string{"wp-content/uploads", "web/app/uploads"}
	case ProjectTypeDrupal:
		symlinks = []string{"web/sites/default/files", "sites/default/files"}
	case ProjectTypeCraft:
		copies = append(copies, ".env.php")
		symlinks = []string{"web/cpresources"}
	case ProjectTypeSymfony:
		copies = append(copies, ".env.local")
	}
	return WorktreeSetupConfig{Symlinks: symlinks, Copies: copies}
}

// ConfigFromSettings extracts a saved WorktreeSetupConfig from a site's settings
// JSON. Falls back to framework defaults for rootPath when nothing is saved.
func ConfigFromSettings(settingsJSON, rootPath string) WorktreeSetupConfig {
	var settingsMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settingsMap); err == nil {
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
			return WorktreeSetupConfig{Symlinks: symlinks, Copies: copies}
		}
	}
	return DefaultWorktreeConfig(DetectProjectType(rootPath))
}

// ResolveWorktreeConfig returns the config that should actually be applied.
// Empty symlink+copy lists mean "use defaults" unless NoShare is set.
func ResolveWorktreeConfig(cfg WorktreeSetupConfig, settingsJSON, rootPath string) WorktreeSetupConfig {
	if cfg.NoShare {
		return WorktreeSetupConfig{NoShare: true}
	}
	if len(cfg.Symlinks) == 0 && len(cfg.Copies) == 0 {
		return ConfigFromSettings(settingsJSON, rootPath)
	}
	return cfg
}

// SlugifyBranch converts a branch name to a URL/directory-safe slug.
// e.g. "feature/my-thing" → "feature-my-thing", "origin/v1.2.3" → "v1-2-3"
func SlugifyBranch(branch string) string {
	slug := strings.ToLower(strings.TrimSpace(branch))
	slug = strings.TrimPrefix(slug, "origin/")
	slug = strings.TrimPrefix(slug, "origin-")

	var b strings.Builder
	prevDash := false
	for _, r := range slug {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "branch"
	}
	return out
}

// CreateGitWorktree creates a new git worktree at dest from mainRepoPath on the given branch.
// If createBranch is true, a new local branch is created.
// After creating the worktree, symlinks and copies from config are set up.
func CreateGitWorktree(mainRepoPath, dest, branch string, createBranch bool, config WorktreeSetupConfig) error {
	args, err := worktreeAddArgs(mainRepoPath, dest, branch, createBranch)
	if err != nil {
		return err
	}
	if _, err := runGit(mainRepoPath, args...); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "already used by worktree") || strings.Contains(msg, "already checked out") {
			return fmt.Errorf("branch %q is already checked out in another worktree; pass create_branch or pick a different branch", branch)
		}
		if strings.Contains(msg, "already exists") {
			return fmt.Errorf("branch %q already exists; omit create_branch to check it out", branch)
		}
		return fmt.Errorf("git worktree add: %w", err)
	}

	SeedWorktreeResources(mainRepoPath, dest, config)
	return nil
}

// SettleGitCheckout waits until path looks like a finished git checkout.
// git worktree add creates the directory, then writes .git / HEAD / files
// across several syscalls; AutoDiscover must not inspect a half-written tree.
func SettleGitCheckout(path string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsGitRepo(path) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if IsLinkedWorktree(path) && GetCurrentBranch(path) == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		// One extra beat so the working tree files land after HEAD.
		time.Sleep(80 * time.Millisecond)
		return
	}
}

// SeedWorktreeResources applies symlink/copy config from parent onto dest.
// Missing sources are skipped. Existing destinations are not overwritten
// (except that empty git-created dirs are replaced for symlink targets).
// Safe to call from AutoDiscover as well as CreateGitWorktree.
func SeedWorktreeResources(parentPath, dest string, config WorktreeSetupConfig) {
	if config.NoShare {
		return
	}
	for _, rel := range config.Symlinks {
		rel = filepath.Clean(rel)
		if !safeRelPath(rel) {
			fmt.Printf("worktree: skip unsafe symlink path %q\n", rel)
			continue
		}
		src := filepath.Join(parentPath, rel)
		dst := filepath.Join(dest, rel)
		if !fileExists(src) {
			continue
		}
		if fileExists(dst) && !isEmptyDir(dst) {
			continue
		}
		_ = os.RemoveAll(dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			fmt.Printf("worktree: mkdir for symlink %s: %v\n", dst, err)
			continue
		}
		if err := os.Symlink(src, dst); err != nil {
			fmt.Printf("worktree: symlink %s → %s: %v\n", src, dst, err)
		}
	}

	for _, rel := range config.Copies {
		rel = filepath.Clean(rel)
		if !safeRelPath(rel) {
			fmt.Printf("worktree: skip unsafe copy path %q\n", rel)
			continue
		}
		src := filepath.Join(parentPath, rel)
		dst := filepath.Join(dest, rel)
		if rel == ".env" && !fileExists(src) {
			src = filepath.Join(parentPath, ".env.example")
		}
		if !fileExists(src) {
			continue
		}
		if fileExists(dst) {
			continue
		}
		if (rel == "vendor" || rel == "node_modules") && !shouldSeedDepDir(parentPath, dest, rel) {
			fmt.Printf("worktree: skip %s — lockfile differs from parent (install deps in the worktree)\n", rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			fmt.Printf("worktree: mkdir for copy %s: %v\n", dst, err)
			continue
		}
		if err := copyPath(src, dst); err != nil {
			fmt.Printf("worktree: copy %s → %s: %v\n", src, dst, err)
		}
	}
}

// worktreeAddArgs builds `git worktree add` arguments, creating a local
// tracking branch when the requested ref is remote-only.
func worktreeAddArgs(repo, dest, branch string, createBranch bool) ([]string, error) {
	if createBranch {
		return []string{"worktree", "add", "-b", branch, dest}, nil
	}
	local := strings.TrimPrefix(branch, "origin/")
	if branchExistsLocal(repo, local) {
		return []string{"worktree", "add", dest, local}, nil
	}
	remoteRef := branch
	if !strings.HasPrefix(branch, "origin/") {
		remoteRef = "origin/" + local
	}
	if branchExists(repo, remoteRef) {
		return []string{"worktree", "add", "--track", "-b", local, dest, remoteRef}, nil
	}
	// Let git try the name as given so the error message is useful.
	return []string{"worktree", "add", dest, branch}, nil
}

func branchExistsLocal(repo, name string) bool {
	_, err := runGit(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

func branchExists(repo, name string) bool {
	_, err := runGit(repo, "rev-parse", "--verify", "--quiet", name)
	return err == nil
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

func isEmptyDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func safeRelPath(rel string) bool {
	if rel == "" || rel == "." || rel == ".." {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)+"..") {
		return false
	}
	return true
}

var jsLockfiles = []string{
	"pnpm-lock.yaml",
	"yarn.lock",
	"bun.lock",
	"bun.lockb",
	"package-lock.json",
	"npm-shrinkwrap.json",
}

// shouldSeedDepDir is true when dest's lockfile matches parent's (or dest has
// no lockfile). Copying a mismatched vendor/ silently loads the wrong packages.
func shouldSeedDepDir(parent, dest, dirName string) bool {
	var names []string
	switch dirName {
	case "vendor":
		names = []string{"composer.lock"}
	case "node_modules":
		names = jsLockfiles
	default:
		return true
	}
	return lockfilesMatch(parent, dest, names)
}

func lockfilesMatch(parent, dest string, names []string) bool {
	for _, name := range names {
		destLock := filepath.Join(dest, name)
		if !fileExists(destLock) {
			continue
		}
		parentLock := filepath.Join(parent, name)
		if !fileExists(parentLock) {
			return false
		}
		pb, err1 := os.ReadFile(parentLock)
		db, err2 := os.ReadFile(destLock)
		if err1 != nil || err2 != nil {
			return false
		}
		return bytes.Equal(pb, db)
	}
	// Dest has none of the named lockfiles — copy is fine.
	return true
}

// copyPath copies a file or directory from src to dst. Prefers `cp -a --reflink=auto`
// so btrfs/xfs can clone extents; falls back to a recursive Go copy.
func copyPath(src, dst string) error {
	_ = os.RemoveAll(dst)
	cmd := exec.Command("cp", "-a", "--reflink=auto", src, dst)
	if out, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		fmt.Printf("worktree: cp --reflink fallback for %s: %v (%s)\n", src, err, bytes.TrimSpace(out))
	}
	return copyPathGo(src, dst)
}

func copyPathGo(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPathGo(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
