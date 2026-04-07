package sites

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/php"
)

// Manager handles site CRUD and keeps Caddy in sync.
type Manager struct {
	db         *dbq.Queries
	caddy      *CaddyClient
	serverRoot string
}

// NewManager creates a Manager.
func NewManager(db *sql.DB, caddy *CaddyClient, serverRoot string) *Manager {
	return &Manager{
		db:         dbq.New(db),
		caddy:      caddy,
		serverRoot: serverRoot,
	}
}

// CreateSiteInput is the data required to create a new site.
type CreateSiteInput struct {
	Domain         string
	RootPath       string
	PHPVersion     string
	Aliases        []string
	HTTPS          bool
	AutoDiscovered bool
	// PublicDir is the subdirectory within RootPath to use as document root (e.g. "public").
	PublicDir string
	// SiteType is "php" (default) or "ws" for a WebSocket reverse-proxy site.
	SiteType string
	// WSUpstream is the dial address used when SiteType == "ws",
	// e.g. "127.0.0.1:7383".
	WSUpstream string
	// ParentSiteID links this site to its parent when it is a git worktree.
	ParentSiteID string
	// WorktreeBranch is the branch this worktree is on.
	WorktreeBranch string
	// ServiceVhost marks this site as a managed service vhost (e.g. reverb.test).
	// Service vhosts are excluded from the user-facing sites list.
	ServiceVhost bool
	// IsGitRepo indicates whether the root path is a git repository.
	IsGitRepo bool
	// GitRemoteURL is the fetch URL for the "origin" remote, if any.
	GitRemoteURL string
	// Framework is the detected project framework (laravel, statamic, wordpress, or "").
	Framework string
}

// Create inserts a site into the DB and provisions its Caddy vhost.
func (m *Manager) Create(ctx context.Context, input CreateSiteInput) (dbq.Site, error) {
	if input.PHPVersion == "" {
		input.PHPVersion = m.latestPHPVersion()
	}
	if input.SiteType == "" {
		input.SiteType = "php"
	}

	aliases, _ := json.Marshal(input.Aliases)
	httpsVal := int64(1)
	if !input.HTTPS {
		httpsVal = 0
	}
	autoVal := int64(0)
	if input.AutoDiscovered {
		autoVal = 1
	}
	serviceVhostVal := int64(0)
	if input.ServiceVhost {
		serviceVhostVal = 1
	}

	// Build settings JSON.
	settingsMap := map[string]string{"site_type": input.SiteType}
	if input.WSUpstream != "" {
		settingsMap["ws_upstream"] = input.WSUpstream
	}
	settingsJSON, _ := json.Marshal(settingsMap)

	var parentID *string
	if input.ParentSiteID != "" {
		parentID = &input.ParentSiteID
	}
	var worktreeBranch *string
	if input.WorktreeBranch != "" {
		worktreeBranch = &input.WorktreeBranch
	}

	id := DomainToID(input.Domain)
	isGitRepoVal := int64(0)
	if input.IsGitRepo {
		isGitRepoVal = 1
	}
	site, err := m.db.CreateSite(ctx, dbq.CreateSiteParams{
		ID:             id,
		Domain:         input.Domain,
		RootPath:       input.RootPath,
		PhpVersion:     input.PHPVersion,
		Aliases:        string(aliases),
		SpxEnabled:     0,
		Https:          httpsVal,
		AutoDiscovered: autoVal,
		Settings:       string(settingsJSON),
		ParentSiteID:   parentID,
		WorktreeBranch: worktreeBranch,
		PublicDir:      input.PublicDir,
		ServiceVhost:   serviceVhostVal,
		IsGitRepo:      isGitRepoVal,
		GitRemoteURL:   input.GitRemoteURL,
		Framework:      input.Framework,
	})
	if err != nil {
		return dbq.Site{}, fmt.Errorf("create site db: %w", err)
	}

	if err := m.syncCaddy(site); err != nil {
		// Log but don't fail — site is in DB, Caddy may not be running yet.
		fmt.Printf("sites: caddy sync error for %s: %v\n", site.Domain, err)
	}

	return site, nil
}

// Delete removes a site from the DB and removes its Caddy vhost.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.db.DeleteSite(ctx, id); err != nil {
		return fmt.Errorf("delete site db: %w", err)
	}

	if err := m.caddy.DeleteVhost("vhost-" + id); err != nil {
		fmt.Printf("sites: caddy delete vhost error for %s: %v\n", id, err)
	}

	return nil
}

// SyncAll loads all sites from DB and provisions them in Caddy.
// Intended to be called on startup.
func (m *Manager) SyncAll(ctx context.Context) error {
	all, err := m.db.GetAllSites(ctx)
	if err != nil {
		return fmt.Errorf("get all sites: %w", err)
	}

	for _, site := range all {
		if err := m.syncCaddy(site); err != nil {
			fmt.Printf("sites: startup sync error for %s: %v\n", site.Domain, err)
		}
	}

	return nil
}

// AutoDiscover creates a site entry for a newly discovered directory.
// The domain is derived from the directory name (e.g. "myapp" → "myapp.test").
// The "server" directory is excluded — it is reserved for devctl's own binaries.
// If the directory is a git linked worktree whose main repo is already tracked,
// the new site's parent_site_id and worktree_branch are set automatically.
func (m *Manager) AutoDiscover(ctx context.Context, dirPath string) error {
	name := filepath.Base(dirPath)

	// "server" is reserved for devctl's own binaries (caddy, meilisearch, etc.).
	if name == "server" {
		return nil
	}

	domain := name + ".test"

	// Skip if already exists.
	if _, err := m.db.GetSiteByDomain(ctx, domain); err == nil {
		return nil // already tracked
	}

	input := CreateSiteInput{
		Domain:         domain,
		RootPath:       dirPath,
		PHPVersion:     m.latestPHPVersion(),
		PublicDir:      DetectPublicDir(dirPath),
		Aliases:        nil,
		HTTPS:          true,
		AutoDiscovered: true,
	}

	// Enrich with git and framework info.
	info := InspectPath(dirPath)
	input.IsGitRepo = info.IsGitRepo
	input.GitRemoteURL = info.GitRemoteURL
	input.Framework = info.Framework
	if input.PublicDir == "" && info.PublicDir != "" {
		input.PublicDir = info.PublicDir
	}

	// Check if this directory is a git linked worktree whose parent is already tracked.
	if IsLinkedWorktree(dirPath) {
		if parentPath, err := GetMainWorktreePath(dirPath); err == nil {
			if parentSite, err := m.db.GetSiteByRootPath(ctx, parentPath); err == nil {
				input.ParentSiteID = parentSite.ID
				input.WorktreeBranch = GetCurrentBranch(dirPath)
				fmt.Printf("sites: auto-discovered %s as worktree of %s (branch: %s)\n",
					domain, parentSite.Domain, input.WorktreeBranch)
			}
		}
	}

	_, err := m.Create(ctx, input)
	return err
}

// CreateWorktree creates a new git worktree for the given parent site on the specified branch,
// places it as a sibling of the parent site directory, registers it as a site in devctl,
// and sets up symlinks/copies per the provided config.
func (m *Manager) CreateWorktree(ctx context.Context, parentID, branch string, createBranch bool, config WorktreeSetupConfig) (dbq.Site, error) {
	parent, err := m.db.GetSite(ctx, parentID)
	if err != nil {
		return dbq.Site{}, fmt.Errorf("parent site not found: %w", err)
	}

	// Find the git root for the parent site.
	if !IsGitRepo(parent.RootPath) {
		return dbq.Site{}, fmt.Errorf("site %q is not a git repository", parent.Domain)
	}
	gitRoot, err := GetGitRoot(parent.RootPath)
	if err != nil {
		return dbq.Site{}, fmt.Errorf("get git root: %w", err)
	}

	// Compute worktree directory and domain from parent name + branch slug.
	parentName := filepath.Base(parent.RootPath)
	branchSlug := SlugifyBranch(branch)
	worktreeDirName := parentName + "-" + branchSlug
	worktreePath := filepath.Join(filepath.Dir(parent.RootPath), worktreeDirName)
	worktreeDomain := worktreeDirName + ".test"

	// Guard: ensure the worktree path is not inside the git root.
	if strings.HasPrefix(worktreePath+string(filepath.Separator), gitRoot+string(filepath.Separator)) {
		return dbq.Site{}, fmt.Errorf("computed worktree path %q is inside git root %q", worktreePath, gitRoot)
	}

	// Check for domain / path conflicts.
	if _, err := m.db.GetSiteByDomain(ctx, worktreeDomain); err == nil {
		return dbq.Site{}, fmt.Errorf("a site with domain %q already exists", worktreeDomain)
	}

	// Determine PHP version from parent settings (inherit).
	phpVersion := parent.PhpVersion

	// Register the site in the DB BEFORE creating the git worktree so that the
	// filesystem watcher (AutoDiscover) sees the domain as already tracked and
	// skips it — avoiding a UNIQUE constraint race when the watcher fires while
	// git is still populating the worktree directory.
	//
	// Git info is inherited from the parent; framework will be re-detected once
	// the worktree is fully populated on first real access. For now inherit
	// the parent's framework so the badge shows immediately.
	site, err := m.Create(ctx, CreateSiteInput{
		Domain:         worktreeDomain,
		RootPath:       worktreePath,
		PHPVersion:     phpVersion,
		PublicDir:      parent.PublicDir,
		HTTPS:          true,
		AutoDiscovered: false,
		ParentSiteID:   parentID,
		WorktreeBranch: branch,
		IsGitRepo:      true,
		GitRemoteURL:   parent.GitRemoteURL,
		Framework:      parent.Framework,
	})
	if err != nil {
		return dbq.Site{}, fmt.Errorf("register worktree site: %w", err)
	}

	// Create the git worktree on disk.
	if err := CreateGitWorktree(gitRoot, worktreePath, branch, createBranch, config); err != nil {
		// Roll back the DB registration.
		_ = m.Delete(ctx, site.ID)
		return dbq.Site{}, fmt.Errorf("create git worktree: %w", err)
	}

	return site, nil
}

// RemoveWorktree removes a git worktree site: deletes the directory, prunes git,
// and removes the site record from devctl.
func (m *Manager) RemoveWorktree(ctx context.Context, worktreeID string) error {
	worktree, err := m.db.GetSite(ctx, worktreeID)
	if err != nil {
		return fmt.Errorf("worktree site not found: %w", err)
	}
	if worktree.ParentSiteID == nil {
		return fmt.Errorf("site %q is not a worktree (no parent_site_id)", worktreeID)
	}

	parent, err := m.db.GetSite(ctx, *worktree.ParentSiteID)
	if err != nil {
		// Parent may have been deleted; still attempt to clean up.
		fmt.Printf("sites: parent site not found for worktree %s, cleaning up anyway\n", worktreeID)
		// Remove just the site record; skip git cleanup since we can't find the repo.
		return m.Delete(ctx, worktreeID)
	}

	gitRoot, err := GetGitRoot(parent.RootPath)
	if err != nil {
		// Git root not accessible; remove site record only.
		fmt.Printf("sites: could not get git root for worktree removal: %v\n", err)
		return m.Delete(ctx, worktreeID)
	}

	if err := RemoveGitWorktree(gitRoot, worktree.RootPath); err != nil {
		fmt.Printf("sites: git worktree remove warning: %v\n", err)
		// Continue to remove the site record even if git cleanup failed.
	}

	return m.Delete(ctx, worktreeID)
}

func (m *Manager) syncCaddy(site dbq.Site) error {
	var aliases []string
	if err := json.Unmarshal([]byte(site.Aliases), &aliases); err != nil {
		aliases = nil
	}

	hosts := []string{site.Domain}
	hosts = append(hosts, aliases...)

	// Parse settings to determine site type.
	var settings map[string]string
	if err := json.Unmarshal([]byte(site.Settings), &settings); err != nil {
		settings = map[string]string{}
	}
	siteType := settings["site_type"]
	wsUpstream := settings["ws_upstream"]

	return m.caddy.UpsertVhost(VhostConfig{
		ID:         "vhost-" + site.ID,
		Hosts:      hosts,
		RootPath:   site.RootPath,
		PublicDir:  site.PublicDir,
		PHPVersion: site.PhpVersion,
		HTTPS:      site.Https == 1,
		SiteType:   siteType,
		WSUpstream: wsUpstream,
		ServerRoot: m.serverRoot,
	})
}

// RemoveServerSite deletes the auto-discovered "server.test" site if it exists.
// "server" is reserved for devctl's own binaries and should never be a vhost.
// This is called once at startup to clean up any previously created entry.
func (m *Manager) RemoveServerSite(ctx context.Context) {
	site, err := m.db.GetSiteByDomain(ctx, "server.test")
	if err != nil {
		return // not found — nothing to do
	}
	if err := m.Delete(ctx, site.ID); err != nil {
		fmt.Printf("sites: failed to remove reserved server.test site: %v\n", err)
	} else {
		fmt.Println("sites: removed reserved server.test site")
	}
}

// DomainToID converts a domain to a safe slug used as the site ID and Caddy @id.
func DomainToID(domain string) string {
	id := strings.ToLower(domain)
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.ReplaceAll(id, "_", "-")
	return id
}

// latestPHPVersion returns the newest installed PHP version string (e.g. "8.4").
// Returns an empty string if no PHP versions are installed or the scan fails.
func (m *Manager) latestPHPVersion() string {
	versions, err := php.InstalledVersions(m.serverRoot)
	if err != nil || len(versions) == 0 {
		return ""
	}
	// InstalledVersions returns versions sorted newest-first.
	return versions[0].Version
}

// PruneStale removes any site whose root_path no longer exists on disk.
// Service vhosts and managed sites are never pruned.
// Intended to be called at startup (e.g. from watcher.scanExisting).
func (m *Manager) PruneStale(ctx context.Context) {
	all, err := m.db.GetAllSites(ctx)
	if err != nil {
		fmt.Printf("sites: prune stale: get all sites: %v\n", err)
		return
	}
	for _, site := range all {
		if site.ServiceVhost != 0 {
			continue // never remove managed service vhosts
		}
		if site.RootPath == "" {
			continue // no filesystem root — treat as a service vhost, never prune
		}
		if _, err := os.Stat(site.RootPath); os.IsNotExist(err) {
			if delErr := m.Delete(ctx, site.ID); delErr != nil {
				fmt.Printf("sites: prune stale: remove %s: %v\n", site.Domain, delErr)
			} else {
				fmt.Printf("sites: pruned stale site %s (path %s no longer exists)\n", site.Domain, site.RootPath)
			}
		}
	}
}

// RemoveIfMissing removes a site from the DB and Caddy if its root_path no
// longer exists on disk. It is a no-op if the path still exists or the site
// is not tracked. Service vhosts are never removed this way.
func (m *Manager) RemoveIfMissing(ctx context.Context, dirPath string) {
	site, err := m.db.GetSiteByRootPath(ctx, dirPath)
	if err != nil {
		// Not tracked — nothing to do.
		return
	}
	if site.ServiceVhost != 0 {
		return
	}
	if _, statErr := os.Stat(dirPath); statErr == nil {
		// Path still exists — don't remove.
		return
	}
	if delErr := m.Delete(ctx, site.ID); delErr != nil {
		fmt.Printf("sites: remove-if-missing: %s: %v\n", site.Domain, delErr)
	} else {
		fmt.Printf("sites: removed site %s (directory %s was deleted)\n", site.Domain, dirPath)
	}
}
