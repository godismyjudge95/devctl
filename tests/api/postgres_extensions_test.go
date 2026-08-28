//go:build integration

package apitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type pgExtStatus struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	FilesInstalled bool   `json:"files_installed"`
	Wired          bool   `json:"wired"`
	Ready          bool   `json:"ready"`
	Version        string `json:"version"`
	Note           string `json:"note"`
	RequiresPeer   string `json:"requires_peer"`
}

func TestPostgresExtensions_ListEndpoint(t *testing.T) {
	body := httpGet(t, "/api/postgres/extensions")
	var exts []pgExtStatus
	if err := json.Unmarshal(body, &exts); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(exts) < 7 {
		t.Fatalf("expected at least timescaledb + pgvector + pg_clickhouse + four search contrib, got %d: %+v", len(exts), exts)
	}
	ids := map[string]bool{}
	for _, e := range exts {
		ids[e.ID] = true
	}
	if !ids["timescaledb"] || !ids["pgvector"] || !ids["pg_clickhouse"] {
		t.Fatalf("missing expected extensions: %+v", exts)
	}
	for _, id := range []string{"pg_trgm", "unaccent", "fuzzystrmatch", "btree_gin"} {
		if !ids[id] {
			t.Fatalf("missing search contrib %s: %+v", id, exts)
		}
	}
}

func TestPostgresExtensions_SettingsEndpoint(t *testing.T) {
	body := httpGet(t, "/api/services/postgres/settings")
	var payload struct {
		Extensions []pgExtStatus `json:"extensions"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(payload.Extensions) < 3 {
		t.Fatalf("expected extensions in postgres settings, got %+v", payload)
	}
}

// TestPostgresExtensions_PgvectorPortable ensures postgres install/ensure rebuilds
// pgvector with OPTFLAGS="" and reports it ready (no SQL wire required).
func TestPostgresExtensions_PgvectorPortable(t *testing.T) {
	const installTimeout = 20 * time.Minute
	ensureInstalledRunning(t, "postgres", installTimeout)

	t.Log("POST /api/postgres/extensions/ensure")
	ensureBody, status := httpPost(t, "/api/postgres/extensions/ensure", nil)
	if status != http.StatusOK {
		t.Fatalf("ensure status %d: %s", status, ensureBody)
	}

	deadline := time.Now().Add(5 * time.Minute)
	var pv *pgExtStatus
	for {
		exts := listPgExts(t)
		pv = findExt(exts, "pgvector")
		if pv != nil && pv.Ready && pv.FilesInstalled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pgvector not ready in time: %+v", pv)
		}
		t.Logf("waiting for pgvector ready: %+v", pv)
		time.Sleep(2 * time.Second)
		_, _ = httpPost(t, "/api/postgres/extensions/ensure", nil)
	}
	if pv.Version == "" {
		t.Fatalf("pgvector missing version: %+v", pv)
	}
}

func TestPostgresExtensions_SearchContribWired(t *testing.T) {
	const installTimeout = 20 * time.Minute
	ensureInstalledRunning(t, "postgres", installTimeout)

	t.Log("POST /api/postgres/extensions/ensure")
	ensureBody, status := httpPost(t, "/api/postgres/extensions/ensure", nil)
	if status != http.StatusOK {
		t.Fatalf("ensure status %d: %s", status, ensureBody)
	}

	wantIDs := []string{"pg_trgm", "unaccent", "fuzzystrmatch", "btree_gin"}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		exts := listPgExts(t)
		allReady := true
		for _, id := range wantIDs {
			e := findExt(exts, id)
			if e == nil || !e.Ready || !e.Wired || !e.FilesInstalled {
				allReady = false
				t.Logf("waiting for %s: %+v", id, e)
			}
		}
		if allReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("search contrib not ready in time: %+v", exts)
		}
		time.Sleep(2 * time.Second)
		_, _ = httpPost(t, "/api/postgres/extensions/ensure", nil)
	}

	simBody, simStatus := httpPost(t, "/api/databases/postgres/query", map[string]any{
		"database": "postgres",
		"sql":      "SELECT similarity('laravel', 'larael')",
		"limit":    1,
	})
	if simStatus != http.StatusOK {
		t.Fatalf("similarity query %d: %s", simStatus, simBody)
	}

	createBody, createStatus := httpPost(t, "/api/databases/postgres/databases", map[string]any{
		"name": "searchcontrib_test",
	})
	if createStatus != http.StatusOK {
		t.Fatalf("create database %d: %s", createStatus, createBody)
	}
	t.Cleanup(func() {
		httpDelete(t, "/api/databases/postgres/databases/searchcontrib_test")
	})

	extBody, extStatus := httpPost(t, "/api/databases/postgres/query", map[string]any{
		"database": "searchcontrib_test",
		"sql":      "SELECT extname FROM pg_extension WHERE extname IN ('pg_trgm','unaccent','fuzzystrmatch','btree_gin') ORDER BY 1",
		"limit":    10,
	})
	if extStatus != http.StatusOK {
		t.Fatalf("new db extensions %d: %s", extStatus, extBody)
	}
	var qres struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(extBody, &qres); err != nil {
		t.Fatalf("decode extension query: %v\n%s", err, extBody)
	}
	got := map[string]bool{}
	for _, row := range qres.Rows {
		if len(row) > 0 {
			got[fmt.Sprint(row[0])] = true
		}
	}
	for _, id := range wantIDs {
		if !got[id] {
			t.Fatalf("new database missing %s: %s", id, extBody)
		}
	}

	ensureBody2, status2 := httpPost(t, "/api/postgres/extensions/ensure", nil)
	if status2 != http.StatusOK {
		t.Fatalf("second ensure status %d: %s", status2, ensureBody2)
	}
}

// TestPostgresExtensions_WireWithClickHouse installs postgres + clickhouse
// (if needed), ensures extensions, and asserts pg_clickhouse becomes ready.
// Compiling pg_clickhouse can take several minutes.
func TestPostgresExtensions_WireWithClickHouse(t *testing.T) {
	const installTimeout = 20 * time.Minute

	ensureInstalledRunning(t, "postgres", installTimeout)
	// Timescale should wire shortly after postgres start.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		ts := findExt(listPgExts(t), "timescaledb")
		if ts != nil && ts.FilesInstalled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timescaledb files not installed: %+v", ts)
		}
		time.Sleep(2 * time.Second)
	}

	ensureInstalledRunning(t, "clickhouse", installTimeout)

	// ensure builds pg_clickhouse if missing and wires template1.
	t.Log("POST /api/postgres/extensions/ensure")
	ensureBody, status := httpPost(t, "/api/postgres/extensions/ensure", nil)
	if status != http.StatusOK {
		t.Fatalf("ensure status %d: %s", status, ensureBody)
	}
	t.Logf("ensure response: %s", ensureBody)

	deadline = time.Now().Add(15 * time.Minute)
	var pch *pgExtStatus
	for {
		exts := listPgExts(t)
		pch = findExt(exts, "pg_clickhouse")
		if pch != nil && pch.Ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pg_clickhouse not ready in time: %+v", pch)
		}
		t.Logf("waiting for pg_clickhouse ready: %+v", pch)
		time.Sleep(5 * time.Second)
		// Re-ensure periodically in case first wire raced server start.
		_, _ = httpPost(t, "/api/postgres/extensions/ensure", nil)
	}

	if !pch.FilesInstalled || !pch.Wired || !pch.Ready {
		t.Fatalf("pg_clickhouse not fully ready: %+v", pch)
	}

	// Idempotent second ensure.
	ensureBody2, status2 := httpPost(t, "/api/postgres/extensions/ensure", nil)
	if status2 != http.StatusOK {
		t.Fatalf("second ensure status %d: %s", status2, ensureBody2)
	}
}

func listPgExts(t *testing.T) []pgExtStatus {
	t.Helper()
	body := httpGet(t, "/api/postgres/extensions")
	var exts []pgExtStatus
	if err := json.Unmarshal(body, &exts); err != nil {
		t.Fatalf("decode extensions: %v\n%s", err, body)
	}
	return exts
}

func findExt(exts []pgExtStatus, id string) *pgExtStatus {
	for i := range exts {
		if exts[i].ID == id {
			return &exts[i]
		}
	}
	return nil
}

func ensureInstalledRunning(t *testing.T, id string, installTimeout time.Duration) {
	t.Helper()
	body := httpGet(t, "/api/services")
	services := decodeJSON[[]ServiceState](t, body)
	for _, svc := range services {
		if svc.ID == id && svc.Installed {
			t.Logf("%s already installed", id)
			if svc.Status != "running" {
				startBody, startStatus := httpPost(t, "/api/services/"+id+"/start", nil)
				if startStatus != http.StatusOK {
					t.Fatalf("start %s: %d %s", id, startStatus, startBody)
				}
			}
			pollServiceStatus(t, id, "running", 2*time.Minute)
			return
		}
	}
	t.Logf("installing %s", id)
	res := httpSSE(t, http.MethodPost, "/api/services/"+id+"/install", installTimeout)
	if res.LastEvent != "done" {
		t.Fatalf("install %s: last event %q data %s", id, res.LastEvent, res.LastData)
	}
	pollServiceInstalled(t, id, true, 30*time.Second)
	pollServiceStatus(t, id, "running", 2*time.Minute)
}
