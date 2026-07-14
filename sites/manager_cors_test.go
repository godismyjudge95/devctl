package sites

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danielgormly/devctl/db"
)

func TestCreate_ServiceVhostEnablesCORS(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	// Caddy admin URL is unreachable — Create logs the sync error and still
	// returns the DB row (same as production when Caddy is briefly down).
	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())

	site, err := m.Create(context.Background(), CreateSiteInput{
		Domain:       "s3.maxio.test",
		SiteType:     "ws",
		WSUpstream:   "127.0.0.1:9900",
		HTTPS:        true,
		CORS:         false, // deliberately off — ServiceVhost must still enable it
		ServiceVhost: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if site.Cors != 1 {
		t.Fatalf("service vhost cors=%d, want 1", site.Cors)
	}
	if site.ServiceVhost != 1 {
		t.Fatalf("service_vhost=%d, want 1", site.ServiceVhost)
	}
}

func TestCreate_UserSiteDefaultsCORSDisabled(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	m := NewManager(sqlDB, NewCaddyClient("http://127.0.0.1:1"), t.TempDir())

	site, err := m.Create(context.Background(), CreateSiteInput{
		Domain:   "myapp.test",
		RootPath: t.TempDir(),
		HTTPS:    true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if site.Cors != 0 {
		t.Fatalf("user site cors=%d, want 0", site.Cors)
	}
	if site.ServiceVhost != 0 {
		t.Fatalf("service_vhost=%d, want 0", site.ServiceVhost)
	}
}
