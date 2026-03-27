//go:build integration

package apitest

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// PHPVersion mirrors one entry from GET /api/php/versions.
type PHPVersion struct {
	Version   string `json:"version"`
	FPMSocket string `json:"fpm_socket"`
	Status    string `json:"status"`
}

// TestSitesCRUD_CreateReadUpdateDelete exercises the full create → read →
// update → delete lifecycle for a site via the REST API.
//
// The test creates a temp directory for the site root so the server-side path
// exists on disk (handleCreateSite inspects it). It is cleaned up in t.Cleanup.
func TestSitesCRUD_CreateReadUpdateDelete(t *testing.T) {
	// ── 0. Create a temp dir to use as the site root ─────────────────────────
	dir, err := os.MkdirTemp("", "devctl-test-site-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	const domain = "test-crud-go.test"

	// ── 1. Pre-condition: ensure no leftover site from a prior run ───────────
	{
		body := httpGet(t, "/api/sites")
		sites := decodeJSON[[]Site](t, body)
		for _, s := range sites {
			if s.Domain == domain {
				t.Logf("pre-condition: site %q exists — deleting before test", domain)
				_, _ = httpDelete(t, "/api/sites/"+s.ID)
			}
		}
	}

	// ── 2. Create ─────────────────────────────────────────────────────────────
	t.Log("step 2: POST /api/sites")
	createBody, createStatus := httpPost(t, "/api/sites", map[string]any{
		"domain":    domain,
		"root_path": dir,
	})
	if createStatus != http.StatusCreated {
		t.Fatalf("create site: expected status 201, got %d: %s", createStatus, string(createBody))
	}

	created := decodeJSON[Site](t, createBody)
	if created.ID == "" {
		t.Fatal("create site: response has empty id")
	}
	if created.Domain != domain {
		t.Errorf("create site: domain = %q, want %q", created.Domain, domain)
	}
	if created.RootPath != dir {
		t.Errorf("create site: root_path = %q, want %q", created.RootPath, dir)
	}

	siteID := created.ID
	t.Cleanup(func() {
		// Best-effort cleanup in case the delete step does not run.
		httpDelete(t, "/api/sites/"+siteID) //nolint:errcheck
	})

	// ── 3. Read — GET /api/sites/{id} ────────────────────────────────────────
	t.Logf("step 3: GET /api/sites/%s", siteID)
	getBody := httpGet(t, "/api/sites/"+siteID)
	fetched := decodeJSON[Site](t, getBody)
	if fetched.ID != siteID {
		t.Errorf("GET site: id = %q, want %q", fetched.ID, siteID)
	}
	if fetched.Domain != domain {
		t.Errorf("GET site: domain = %q, want %q", fetched.Domain, domain)
	}

	// ── 4. Read — GET /api/sites list contains our site ──────────────────────
	t.Log("step 4: GET /api/sites list contains created site")
	listBody := httpGet(t, "/api/sites")
	allSites := decodeJSON[[]Site](t, listBody)
	found := false
	for _, s := range allSites {
		if s.ID == siteID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GET /api/sites: site %q (id=%s) not found in list", domain, siteID)
	}

	// ── 5. Update — PUT /api/sites/{id} ──────────────────────────────────────
	t.Logf("step 5: PUT /api/sites/%s", siteID)
	updatedDomain := fmt.Sprintf("test-crud-go-updated-%s.test", siteID[:8])
	updateBody, updateStatus := httpPut(t, "/api/sites/"+siteID, map[string]any{
		"domain":    updatedDomain,
		"root_path": dir,
	})
	if updateStatus != http.StatusOK {
		t.Fatalf("update site: expected status 200, got %d: %s", updateStatus, string(updateBody))
	}

	updated := decodeJSON[Site](t, updateBody)
	if updated.Domain != updatedDomain {
		t.Errorf("update site: domain = %q, want %q", updated.Domain, updatedDomain)
	}

	// Re-fetch to confirm persistence.
	reread := decodeJSON[Site](t, httpGet(t, "/api/sites/"+siteID))
	if reread.Domain != updatedDomain {
		t.Errorf("re-read after update: domain = %q, want %q", reread.Domain, updatedDomain)
	}

	// ── 6. Delete — DELETE /api/sites/{id} ───────────────────────────────────
	t.Logf("step 6: DELETE /api/sites/%s", siteID)
	_, deleteStatus := httpDelete(t, "/api/sites/"+siteID)
	if deleteStatus != http.StatusNoContent {
		t.Fatalf("delete site: expected status 204, got %d", deleteStatus)
	}

	// ── 7. Verify 404 after delete ────────────────────────────────────────────
	t.Log("step 7: verify site is gone")
	httpGetStatus(t, "/api/sites/"+siteID, http.StatusNotFound)

	// Also verify the list no longer contains the site.
	listBody2 := httpGet(t, "/api/sites")
	allSites2 := decodeJSON[[]Site](t, listBody2)
	for _, s := range allSites2 {
		if s.ID == siteID {
			t.Errorf("after delete: site %q (id=%s) still present in list", domain, siteID)
		}
	}
}

// TestSites_Create_MissingDomain_Returns400 verifies validation: domain is required.
func TestSites_Create_MissingDomain_Returns400(t *testing.T) {
	dir, err := os.MkdirTemp("", "devctl-test-site-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	_, status := httpPost(t, "/api/sites", map[string]any{
		"root_path": dir,
	})
	if status != http.StatusBadRequest {
		t.Errorf("missing domain: expected status 400, got %d", status)
	}
}

// TestSites_Create_MissingRootPath_Returns400 verifies validation: root_path is required.
func TestSites_Create_MissingRootPath_Returns400(t *testing.T) {
	_, status := httpPost(t, "/api/sites", map[string]any{
		"domain": "test-validation-only.test",
	})
	if status != http.StatusBadRequest {
		t.Errorf("missing root_path: expected status 400, got %d", status)
	}
}

// TestSites_GetUnknown_Returns404 verifies that fetching a non-existent site
// returns HTTP 404.
func TestSites_GetUnknown_Returns404(t *testing.T) {
	httpGetStatus(t, "/api/sites/nonexistent-site-id-zzzz", http.StatusNotFound)
}

// TestSites_StartupPrune_RemovesStaleSite verifies that if a site's root_path
// directory no longer exists on disk, devctl removes it from the database on
// the next startup (i.e., the watcher's scanExisting prune step).
//
// The test registers a site with a non-existent path, restarts the devctl
// service (requires systemd inside the container), waits for devctl to respond,
// then confirms the site is no longer present.
func TestSites_StartupPrune_RemovesStaleSite(t *testing.T) {
	// ── 0. Create a site with a path that does NOT exist on disk ─────────────
	const domain = "test-stale-prune.test"
	const fakePath = "/tmp/devctl-stale-prune-nonexistent-dir"

	// Remove any leftover from a prior run.
	{
		body := httpGet(t, "/api/sites")
		for _, s := range decodeJSON[[]Site](t, body) {
			if s.Domain == domain {
				t.Logf("pre-condition: removing leftover site %q", domain)
				httpDelete(t, "/api/sites/"+s.ID) //nolint:errcheck
			}
		}
	}

	// Make sure fakePath does not exist.
	os.RemoveAll(fakePath)

	// Register the site; the API allows any path.
	createBody, createStatus := httpPost(t, "/api/sites", map[string]any{
		"domain":    domain,
		"root_path": fakePath,
	})
	if createStatus != http.StatusCreated {
		t.Fatalf("create stale site: expected 201, got %d: %s", createStatus, string(createBody))
	}
	created := decodeJSON[Site](t, createBody)
	t.Logf("registered stale site id=%s domain=%s root_path=%s", created.ID, created.Domain, created.RootPath)

	// Verify it is visible before the restart.
	before := httpGet(t, "/api/sites")
	foundBefore := false
	for _, s := range decodeJSON[[]Site](t, before) {
		if s.ID == created.ID {
			foundBefore = true
			break
		}
	}
	if !foundBefore {
		t.Fatal("pre-restart: stale site not found in list — cannot proceed")
	}

	// ── 1. Restart devctl ─────────────────────────────────────────────────────
	t.Log("step 1: restart devctl via systemctl")
	if err := exec.Command("systemctl", "restart", "devctl").Run(); err != nil {
		t.Fatalf("systemctl restart devctl: %v", err)
	}

	// ── 2. Wait for devctl to respond again ───────────────────────────────────
	t.Log("step 2: waiting for devctl to come back up")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL() + "/api/sites") //nolint:noctx
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}

	// ── 3. Assert the stale site is gone ─────────────────────────────────────
	t.Log("step 3: assert stale site was pruned")
	after := httpGet(t, "/api/sites")
	for _, s := range decodeJSON[[]Site](t, after) {
		if s.ID == created.ID || s.Domain == domain {
			t.Errorf("stale site %q (id=%s) still present after restart — startup prune did not run", domain, created.ID)
		}
	}
}

// pollSiteGone polls GET /api/sites until the site with the given ID is absent,
// or until the timeout is exceeded. It fails the test if the site is still
// present after timeout.
func pollSiteGone(t *testing.T, siteID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/sites")
		found := false
		for _, s := range decodeJSON[[]Site](t, body) {
			if s.ID == siteID {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Final assertion.
	body := httpGet(t, "/api/sites")
	for _, s := range decodeJSON[[]Site](t, body) {
		if s.ID == siteID {
			t.Errorf("site %q still present after %v — fsnotify Remove did not trigger cleanup", siteID, timeout)
			return
		}
	}
}

// pollSiteByDomain polls GET /api/sites until a site with the given domain
// appears, or until the timeout is exceeded. It returns the Site if found, or
// fails the test.
func pollSiteByDomain(t *testing.T, domain string, timeout time.Duration) Site {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body := httpGet(t, "/api/sites")
		for _, s := range decodeJSON[[]Site](t, body) {
			if s.Domain == domain {
				return s
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("site with domain %q not found after %v", domain, timeout)
	return Site{}
}

// TestSites_RuntimeRemoval_RemovesSiteWhenDirectoryDeleted verifies that when
// a site directory is removed from disk while devctl is running, the site is
// automatically removed from the database within a few seconds.
//
// The test creates a real directory inside the sites watch dir, lets the watcher
// auto-discover it, then deletes the directory and waits for the site to disappear.
func TestSites_RuntimeRemoval_RemovesSiteWhenDirectoryDeleted(t *testing.T) {
	// ── 0. Determine the watch dir ────────────────────────────────────────────
	// The watcher watches the parent of DEVCTL_SERVER_ROOT (i.e. ~/ddev/sites/).
	serverRoot := os.Getenv("DEVCTL_SERVER_ROOT")
	if serverRoot == "" {
		serverRoot = "/home/testuser/ddev/sites/server"
	}
	watchDir := filepath.Dir(serverRoot)

	// ── 1. Create a temp dir under the watch dir ──────────────────────────────
	siteDir, err := os.MkdirTemp(watchDir, "devctl-test-runtime-removal-*")
	if err != nil {
		t.Fatalf("create site dir under watch dir: %v", err)
	}
	// The watcher derives domain as <dirname>.test.
	domain := filepath.Base(siteDir) + ".test"

	t.Logf("created site dir: %s  domain: %s", siteDir, domain)

	// Cleanup in case the test fails mid-way.
	t.Cleanup(func() { os.RemoveAll(siteDir) })

	// ── 2. Wait for auto-discovery ───────────────────────────────────────────
	// The fsnotify Create event should register the site within a few seconds.
	t.Log("step 2: waiting for watcher to auto-discover site")
	discovered := pollSiteByDomain(t, domain, 10*time.Second)
	t.Logf("auto-discovered site id=%s", discovered.ID)

	// Cleanup the DB entry in case the test fails after this point.
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+discovered.ID) }) //nolint:errcheck

	// ── 3. Delete the directory ───────────────────────────────────────────────
	t.Log("step 3: removing site directory")
	if err := os.RemoveAll(siteDir); err != nil {
		t.Fatalf("remove site dir: %v", err)
	}

	// ── 4. Wait for devctl to notice and remove the site ─────────────────────
	t.Log("step 4: waiting for site to disappear from API")
	pollSiteGone(t, discovered.ID, 10*time.Second)
}

// TestSites_AutoDiscover_UsesLatestPHPVersion verifies that when a site is
// created without an explicit php_version, devctl assigns the latest installed
// PHP version rather than a hardcoded default.
//
// The test reads the currently installed PHP versions from GET /api/php/versions
// (sorted newest-first by the API) and creates a site without specifying
// php_version. The site's php_version must equal the latest installed version.
// If no PHP versions are installed the test expects an empty string.
func TestSites_AutoDiscover_UsesLatestPHPVersion(t *testing.T) {
	// ── 0. Determine the expected PHP version ────────────────────────────────
	versionsBody := httpGet(t, "/api/php/versions")
	versions := decodeJSON[[]PHPVersion](t, versionsBody)

	wantPHP := ""
	if len(versions) > 0 {
		// API returns versions sorted newest-first; the first element is "latest".
		wantPHP = versions[0].Version
	}

	if wantPHP == "" {
		t.Log("no PHP versions installed in this container — expecting empty php_version")
	} else {
		t.Logf("latest installed PHP version: %s", wantPHP)
	}

	// ── 1. Create a temp dir to use as the site root ─────────────────────────
	dir, err := os.MkdirTemp("", "devctl-test-autodiscover-php-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	const domain = "test-autodiscover-php.test"

	// ── 2. Pre-condition: remove any leftover site ────────────────────────────
	{
		body := httpGet(t, "/api/sites")
		sites := decodeJSON[[]Site](t, body)
		for _, s := range sites {
			if s.Domain == domain {
				t.Logf("pre-condition: removing leftover site %q", domain)
				_, _ = httpDelete(t, "/api/sites/"+s.ID)
			}
		}
	}

	// ── 3. Create the site WITHOUT specifying php_version ────────────────────
	t.Log("step 3: POST /api/sites without php_version")
	createBody, createStatus := httpPost(t, "/api/sites", map[string]any{
		"domain":    domain,
		"root_path": dir,
		// Deliberately omit "php_version" to exercise the default-assignment path.
	})
	if createStatus != http.StatusCreated {
		t.Fatalf("create site: expected 201, got %d: %s", createStatus, string(createBody))
	}

	created := decodeJSON[Site](t, createBody)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+created.ID) }) //nolint:errcheck

	// ── 4. Assert php_version equals the latest installed version ────────────
	t.Logf("step 4: assert php_version = %q", wantPHP)
	if created.PhpVersion != wantPHP {
		t.Errorf("site php_version = %q, want %q (latest installed)", created.PhpVersion, wantPHP)
	}

	// ── 5. Re-fetch to confirm the value is persisted in the DB ──────────────
	t.Logf("step 5: re-fetch GET /api/sites/%s", created.ID)
	fetched := decodeJSON[Site](t, httpGet(t, fmt.Sprintf("/api/sites/%s", created.ID)))
	if fetched.PhpVersion != wantPHP {
		t.Errorf("re-fetched site php_version = %q, want %q", fetched.PhpVersion, wantPHP)
	}
}
