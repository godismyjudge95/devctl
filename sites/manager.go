package sites

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
)

// Manager handles site CRUD and keeps Caddy in sync.
type Manager struct {
	db    *dbq.Queries
	caddy *CaddyClient
}

// NewManager creates a Manager.
func NewManager(db *sql.DB, caddy *CaddyClient) *Manager {
	return &Manager{
		db:    dbq.New(db),
		caddy: caddy,
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
	// SiteType is "php" (default) or "ws" for a WebSocket reverse-proxy site.
	SiteType string
	// WSUpstream is the dial address used when SiteType == "ws",
	// e.g. "127.0.0.1:7383".
	WSUpstream string
}

// Create inserts a site into the DB and provisions its Caddy vhost.
func (m *Manager) Create(ctx context.Context, input CreateSiteInput) (dbq.Site, error) {
	if input.PHPVersion == "" {
		input.PHPVersion = "8.3"
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

	// Build settings JSON.
	settingsMap := map[string]string{"site_type": input.SiteType}
	if input.WSUpstream != "" {
		settingsMap["ws_upstream"] = input.WSUpstream
	}
	settingsJSON, _ := json.Marshal(settingsMap)

	id := DomainToID(input.Domain)
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

	_, err := m.Create(ctx, CreateSiteInput{
		Domain:         domain,
		RootPath:       dirPath,
		PHPVersion:     "8.3",
		Aliases:        nil,
		HTTPS:          true,
		AutoDiscovered: true,
	})
	return err
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
		PHPVersion: site.PhpVersion,
		HTTPS:      site.Https == 1,
		SiteType:   siteType,
		WSUpstream: wsUpstream,
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
