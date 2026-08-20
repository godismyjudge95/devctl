//go:build integration

package apitest

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type enginesResp struct {
	Engines []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Kind      string `json:"kind"`
		Installed bool   `json:"installed"`
		Running   bool   `json:"running"`
	} `json:"engines"`
}

func TestListDatabaseEngines(t *testing.T) {
	body := httpGet(t, "/api/databases")
	res := decodeJSON[enginesResp](t, body)
	if len(res.Engines) < 4 {
		t.Fatalf("expected mysql, postgres, clickhouse, sqlite — got %d", len(res.Engines))
	}
	ids := map[string]bool{}
	for _, e := range res.Engines {
		ids[e.ID] = true
		if e.Label == "" {
			t.Errorf("%s missing label", e.ID)
		}
	}
	for _, id := range []string{"mysql", "postgres", "clickhouse", "sqlite"} {
		if !ids[id] {
			t.Errorf("missing engine %s", id)
		}
	}
}

func TestUnknownDatabaseEngine(t *testing.T) {
	httpGetStatus(t, "/api/databases/oracle/databases", 404)
}

func TestStoppedOrMissingEngine(t *testing.T) {
	body := httpGet(t, "/api/databases")
	res := decodeJSON[enginesResp](t, body)
	for _, e := range res.Engines {
		if e.ID == "sqlite" {
			continue
		}
		if !e.Installed || !e.Running {
			b, status := httpGetRaw(t, "/api/databases/"+e.ID+"/databases")
			if status == 200 {
				t.Fatalf("%s is not running/installed but list returned 200: %s", e.ID, b)
			}
			if status != 404 && status != 503 {
				t.Fatalf("%s: expected 404 or 503, got %d (%s)", e.ID, status, b)
			}
			return
		}
	}
}

func TestSQLiteDatabaseCRUD(t *testing.T) {
	sitesRoot := os.Getenv("DEVCTL_SITES_ROOT")
	if sitesRoot == "" {
		sitesRoot = "/home/testuser/ddev/sites"
	}
	siteDir := filepath.Join(sitesRoot, "dbbrowser-test", "database")
	if err := os.MkdirAll(siteDir, 0777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Daemon runs as testuser and must be able to write WAL/SHM next to the file.
	_ = os.Chmod(filepath.Join(sitesRoot, "dbbrowser-test"), 0777)
	_ = os.Chmod(siteDir, 0777)
	dbPath := filepath.Join(siteDir, "database.sqlite")
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(sitesRoot, "dbbrowser-test"))
	})

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS probe (id INTEGER PRIMARY KEY)`); err != nil {
		sqlDB.Close()
		t.Fatalf("init sqlite: %v", err)
	}
	sqlDB.Close()
	_ = os.Chmod(dbPath, 0666)

	body := httpGet(t, "/api/databases")
	res := decodeJSON[enginesResp](t, body)
	var sqliteRunning bool
	for _, e := range res.Engines {
		if e.ID == "sqlite" && e.Running {
			sqliteRunning = true
		}
	}
	if !sqliteRunning {
		t.Fatalf("expected sqlite engine to be running after creating %s", dbPath)
	}

	catBody := httpGet(t, "/api/databases/sqlite/databases")
	var catalogs struct {
		Databases []struct {
			Name string `json:"name"`
		} `json:"databases"`
	}
	if err := json.Unmarshal(catBody, &catalogs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range catalogs.Databases {
		if d.Name == "dbbrowser-test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sqlite catalogs missing dbbrowser-test: %s", catBody)
	}

	tablesBody := httpGet(t, "/api/databases/sqlite/tables?database=dbbrowser-test")
	var tables struct {
		Tables []struct {
			Name string `json:"name"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(tablesBody, &tables); err != nil {
		t.Fatal(err)
	}

	// Create a table via API
	_, status := httpPost(t, "/api/databases/sqlite/tables", map[string]any{
		"database": "dbbrowser-test",
		"name":     "users",
		"columns": []map[string]any{
			{"name": "id", "type": "INTEGER", "primary_key": true, "auto_increment": true, "nullable": false},
			{"name": "name", "type": "TEXT", "nullable": false},
			{"name": "email", "type": "TEXT", "nullable": true},
		},
	})
	if status != 200 {
		t.Fatalf("create table: %d", status)
	}

	_, status = httpPost(t, "/api/databases/sqlite/rows", map[string]any{
		"database": "dbbrowser-test",
		"table":    "users",
		"values":   map[string]any{"name": "Ada", "email": "ada@example.test"},
	})
	if status != 200 {
		t.Fatalf("insert: %d", status)
	}

	rowsBody := httpGet(t, "/api/databases/sqlite/rows?database=dbbrowser-test&table=users")
	var rows struct {
		Total      int             `json:"total"`
		PrimaryKey []string        `json:"primary_key"`
		Rows       [][]any         `json:"rows"`
		Columns    []map[string]any `json:"columns"`
	}
	if err := json.Unmarshal(rowsBody, &rows); err != nil {
		t.Fatal(err)
	}
	if rows.Total != 1 {
		t.Fatalf("total = %d, body=%s", rows.Total, rowsBody)
	}
	if len(rows.PrimaryKey) == 0 || rows.PrimaryKey[0] != "id" {
		t.Fatalf("pk = %+v", rows.PrimaryKey)
	}

	queryBody, status := httpPost(t, "/api/databases/sqlite/query", map[string]any{
		"database": "dbbrowser-test",
		"sql":      "SELECT name FROM users",
		"limit":    50,
	})
	if status != 200 {
		t.Fatalf("query: %d %s", status, queryBody)
	}
	var qres struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(queryBody, &qres); err != nil {
		t.Fatal(err)
	}
	if len(qres.Rows) != 1 {
		t.Fatalf("query rows = %d %s", len(qres.Rows), queryBody)
	}

	_, status = httpPost(t, "/api/databases/sqlite/truncate", map[string]any{
		"database": "dbbrowser-test",
		"table":    "users",
	})
	if status != 200 {
		t.Fatalf("truncate: %d", status)
	}

	_, status = httpDeleteJSON(t, "/api/databases/sqlite/tables", map[string]any{
		"database": "dbbrowser-test",
		"table":    "users",
	})
	if status != 200 {
		t.Fatalf("drop table: %d", status)
	}
}

func TestSQLiteExportAndColumns(t *testing.T) {
	sitesRoot := os.Getenv("DEVCTL_SITES_ROOT")
	if sitesRoot == "" {
		sitesRoot = "/home/testuser/ddev/sites"
	}
	siteDir := filepath.Join(sitesRoot, "dbbrowser-test", "database")
	if err := os.MkdirAll(siteDir, 0777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(sitesRoot, "dbbrowser-test"), 0777)
	_ = os.Chmod(siteDir, 0777)
	dbPath := filepath.Join(siteDir, "database.sqlite")
	t.Cleanup(func() { os.RemoveAll(filepath.Join(sitesRoot, "dbbrowser-test")) })
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		sqlDB.Close()
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (name) VALUES ('Ada')`); err != nil {
		sqlDB.Close()
		t.Fatal(err)
	}
	sqlDB.Close()
	_ = os.Chmod(dbPath, 0666)

	body, status := httpPost(t, "/api/databases/sqlite/export", map[string]any{
		"database": "dbbrowser-test",
		"table":    "users",
		"format":   "csv",
	})
	if status != 200 {
		t.Fatalf("export: %d %s", status, body)
	}
	if !strings.Contains(string(body), "name") {
		t.Fatalf("csv body: %s", body)
	}

	_, status = httpPost(t, "/api/databases/sqlite/columns", map[string]any{
		"database": "dbbrowser-test",
		"table":    "users",
		"column":   map[string]any{"name": "bio", "type": "TEXT", "nullable": true},
	})
	if status != 200 {
		t.Fatalf("add column: %d", status)
	}
}

func TestCreateDatabaseValidation(t *testing.T) {
	_, status := httpPost(t, "/api/databases/sqlite/databases", map[string]any{})
	if status != 400 && status != 404 && status != 503 {
		t.Fatalf("empty name should fail, got %d", status)
	}
}
