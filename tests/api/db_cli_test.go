//go:build integration

package apitest

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// devctlCLI runs the devctl CLI in dir. stdin is nil for /dev/null, which is
// not a terminal.
func devctlCLI(t *testing.T, dir string, stdin io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command("devctl", args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func devctlCLIOK(t *testing.T, dir string, stdin io.Reader, args ...string) string {
	t.Helper()
	out, errOut, err := devctlCLI(t, dir, stdin, args...)
	if err != nil {
		t.Fatalf("devctl %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out, errOut)
	}
	return out
}

type cliDBRow struct {
	Engine   string `json:"engine"`
	Database string `json:"database"`
	System   bool   `json:"system"`
}

func cliListHas(t *testing.T, alias, id, name string) bool {
	t.Helper()
	var rows []cliDBRow
	if err := json.Unmarshal([]byte(devctlCLIOK(t, "/", nil, "db:list", alias, "--json")), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Engine == id && r.Database == name {
			return true
		}
	}
	return false
}

func cliFirstCell(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var res struct {
		Rows [][]any `json:"rows"`
	}
	out := devctlCLIOK(t, dir, nil, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		t.Fatalf("db:query %v: %v\n%s", args, err, out)
	}
	return fmt.Sprint(res.Rows[0][0])
}

func waitForSiteAt(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
			if s.RootPath == root {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no site was discovered at %s", root)
}

// TestDBCLI_SQLiteSiteDefaults runs db:* commands inside a Laravel-style site
// with DB_CONNECTION=sqlite, so engine and name come from its .env.
func TestDBCLI_SQLiteSiteDefaults(t *testing.T) {
	sitesRoot := os.Getenv("DEVCTL_SITES_ROOT")
	if sitesRoot == "" {
		sitesRoot = "/home/testuser/ddev/sites"
	}
	name := "dbcli-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	siteDir := filepath.Join(sitesRoot, name)
	dbDir := filepath.Join(siteDir, "database")
	if err := os.MkdirAll(dbDir, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(siteDir) })
	_ = os.Chmod(siteDir, 0o777)
	_ = os.Chmod(dbDir, 0o777)
	dbFile := filepath.Join(dbDir, "database.sqlite")
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY); INSERT INTO probe (id) VALUES (1), (2);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(dbFile, 0o666)
	if err := os.WriteFile(filepath.Join(siteDir, ".env"), []byte("APP_NAME=dbcli\nDB_CONNECTION=sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForSiteAt(t, siteDir)

	var conn struct {
		Engine   string `json:"engine"`
		Name     string `json:"name"`
		Database string `json:"database"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(devctlCLIOK(t, siteDir, nil, "db:connection", "--json")), &conn); err != nil {
		t.Fatal(err)
	}
	if conn.Engine != "sqlite" || conn.Name != name || conn.Database != dbFile || conn.URL != "sqlite://"+dbFile {
		t.Fatalf("db:connection = %+v, want sqlite %s at %s", conn, name, dbFile)
	}

	if n := cliFirstCell(t, siteDir, "db:query", "SELECT COUNT(*) AS n FROM probe"); n != "2" {
		t.Fatalf("db:query in the site dir counted %s rows, want 2", n)
	}
	if n := cliFirstCell(t, filepath.Join(siteDir, "database"), "db:query", "SELECT MAX(id) FROM probe"); n != "2" {
		t.Fatalf("db:query in a site subdirectory: max id %s, want 2", n)
	}
	devctlCLIOK(t, "/", nil, "db:query", "sqlite", name, "INSERT INTO probe (id) VALUES (3)")
	if n := cliFirstCell(t, "/", "db:query", "sqlite", name, "SELECT COUNT(*) FROM probe"); n != "3" {
		t.Fatalf("explicit engine and name: counted %s rows, want 3", n)
	}
	if !cliListHas(t, "sqlite", "sqlite", name) {
		t.Fatalf("db:list sqlite is missing %s", name)
	}

	if _, errOut, err := devctlCLI(t, siteDir, nil, "db:drop"); err == nil || !strings.Contains(errOut, "--force") {
		t.Fatalf("db:drop with stdin not a terminal: want a --force refusal, got err=%v stderr=%s", err, errOut)
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("refused drop removed the database: %v", err)
	}

	if _, errOut, err := devctlCLI(t, "/", nil, "db:query", "SELECT 1"); err == nil || !strings.Contains(errOut, "<engine> <name>") {
		t.Fatalf("db:query outside a site without engine: want a hint, got err=%v stderr=%s", err, errOut)
	}
}

// testDBCLIRoundTrip drives every db:* command against one engine. id is the
// API engine ID; alias is the name passed on the command line.
func testDBCLIRoundTrip(t *testing.T, id, alias, systemDB string) {
	ensureInstalledRunning(t, id, 20*time.Minute)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	src, dup, scratch := "devctl_cli_"+suffix, "devctl_cli_copy_"+suffix, "devctl_cli_scratch_"+suffix
	for _, name := range []string{src, dup, scratch} {
		dropDatabaseCleanup(t, id, name)
	}
	dir := t.TempDir()
	dumpFile := filepath.Join(dir, "fixture.sql.gz")
	if err := os.WriteFile(dumpFile, gzipBytes(t, []byte(dumpFixture)).Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, errOut, err := devctlCLI(t, dir, nil, "db:import", alias, src, dumpFile); err == nil || !strings.Contains(errOut, "does not exist") {
		t.Fatalf("import into a missing database without --create: got err=%v stderr=%s", err, errOut)
	}
	var imp struct {
		Engine   string `json:"engine"`
		Database string `json:"database"`
		Bytes    int64  `json:"bytes"`
		Created  bool   `json:"created"`
	}
	if err := json.Unmarshal([]byte(devctlCLIOK(t, dir, nil, "db:import", alias, src, dumpFile, "--create", "--json")), &imp); err != nil {
		t.Fatal(err)
	}
	if imp.Engine != id || imp.Database != src || !imp.Created || imp.Bytes != int64(len(dumpFixture)) {
		t.Fatalf("db:import --create = %+v", imp)
	}

	// Duplicate before any query: Postgres copies with TEMPLATE, which fails
	// while a pooled daemon connection to the source is open.
	devctlCLIOK(t, dir, nil, "db:duplicate", alias, src, dup, "--json")

	for _, name := range []string{src, dup} {
		if n := cliFirstCell(t, dir, "db:query", alias, name, "SELECT COUNT(*) FROM people"); n != "3" {
			t.Fatalf("%s has %s rows, want 3", name, n)
		}
	}

	stdout := devctlCLIOK(t, dir, nil, "db:export", alias, src)
	if !strings.Contains(stdout, "people") || !strings.Contains(stdout, "Grace") {
		t.Fatalf("db:export to stdout is missing the table or its rows:\n%s", stdout)
	}
	outFile := filepath.Join(dir, "out.sql")
	var exp struct {
		File  string `json:"file"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(devctlCLIOK(t, dir, nil, "db:export", alias, src, outFile, "--json")), &exp); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(outFile)
	if err != nil || exp.Bytes != int64(len(written)) || !bytes.Contains(written, []byte("Grace")) {
		t.Fatalf("db:export to a file: %+v, read %d bytes, err %v", exp, len(written), err)
	}

	devctlCLIOK(t, dir, nil, "db:query", alias, dup, "DROP TABLE people")
	f, err := os.Open(outFile)
	if err != nil {
		t.Fatal(err)
	}
	devctlCLIOK(t, dir, f, "db:import", alias, dup, "-")
	f.Close()
	if n := cliFirstCell(t, dir, "db:query", alias, dup, "SELECT COUNT(*) FROM people"); n != "3" {
		t.Fatalf("re-import from stdin: %s rows, want 3", n)
	}

	// A fresh database: Postgres refuses DROP DATABASE while the daemon pool
	// holds a connection to it.
	devctlCLIOK(t, dir, nil, "db:create", alias, scratch)
	if !cliListHas(t, alias, id, scratch) {
		t.Fatalf("db:create: %s is not listed", scratch)
	}
	if _, errOut, err := devctlCLI(t, dir, nil, "db:drop", alias, scratch); err == nil || !strings.Contains(errOut, "--force") {
		t.Fatalf("db:drop with stdin not a terminal: want a --force refusal, got err=%v stderr=%s", err, errOut)
	}
	if !cliListHas(t, alias, id, scratch) {
		t.Fatalf("refused drop removed %s", scratch)
	}
	devctlCLIOK(t, dir, nil, "db:drop", alias, scratch, "--force")
	if cliListHas(t, alias, id, scratch) {
		t.Fatalf("db:drop --force left %s", scratch)
	}

	if _, _, err := devctlCLI(t, dir, nil, "db:drop", alias, systemDB, "--force"); err == nil {
		t.Fatalf("db:drop of system database %s: want an error", systemDB)
	}
	if !cliListHas(t, alias, id, systemDB) {
		t.Fatalf("system database %s is gone", systemDB)
	}
}

func TestDBCLI_MySQLRoundTrip(t *testing.T) {
	testDBCLIRoundTrip(t, "mysql", "mysql", "mysql")
}

func TestDBCLI_PostgresRoundTrip(t *testing.T) {
	testDBCLIRoundTrip(t, "postgres", "pgsql", "postgres")
}
