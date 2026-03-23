//go:build integration

package apitest

import (
	"testing"
)

// Site mirrors the db Site row shape returned by GET /api/sites.
// Fields that may be null in the database are represented as strings here;
// the JSON decoder will leave them as the zero value ("") when absent/null.
type Site struct {
	ID             string `json:"id"`
	Domain         string `json:"domain"`
	RootPath       string `json:"root_path"`
	PhpVersion     string `json:"php_version"`
	SpxEnabled     int    `json:"spx_enabled"`
	HTTPS          int    `json:"https"`
	AutoDiscovered int    `json:"auto_discovered"`
	PublicDir      string `json:"public_dir"`
	Framework      string `json:"framework"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// TestGetSites_StatusOK verifies that GET /api/sites returns HTTP 200.
func TestGetSites_StatusOK(t *testing.T) {
	httpGet(t, "/api/sites")
}

// TestGetSites_UnmarshalsToSlice verifies the response body can be decoded
// into a []Site without error. An empty slice is valid in a fresh container.
func TestGetSites_UnmarshalsToSlice(t *testing.T) {
	body := httpGet(t, "/api/sites")
	_ = decodeJSON[[]Site](t, body)
}

// TestGetSites_IfPopulated_FieldsAreNonEmpty verifies that any sites returned
// have non-empty id and domain fields. If the list is empty the test is skipped.
func TestGetSites_IfPopulated_FieldsAreNonEmpty(t *testing.T) {
	body := httpGet(t, "/api/sites")
	sites := decodeJSON[[]Site](t, body)

	for i, s := range sites {
		if s.ID == "" {
			t.Errorf("site[%d]: id is empty", i)
		}
		if s.Domain == "" {
			t.Errorf("site[%d] (id=%q): domain is empty", i, s.ID)
		}
	}
}

// TestGetSitesDetect_StatusOK verifies that GET /api/sites/detect returns HTTP 200
// when a valid root_path is supplied.
func TestGetSitesDetect_StatusOK(t *testing.T) {
	httpGet(t, "/api/sites/detect?root_path=/tmp")
}

// TestGetSitesDetect_MissingRootPath verifies that omitting root_path returns 400.
func TestGetSitesDetect_MissingRootPath(t *testing.T) {
	httpGetStatus(t, "/api/sites/detect", 400)
}
