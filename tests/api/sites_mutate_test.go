//go:build integration

package apitest

import (
	"fmt"
	"net/http"
	"os"
	"testing"
)

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
