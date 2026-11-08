package cli

import (
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDBCommands_AreRegistered(t *testing.T) {
	for _, tc := range []cmdCheck{
		{name: "db:list", wantArgs: true},
		{name: "db:create", wantArgs: true},
		{name: "db:drop", wantArgs: true, wantFlag: "force"},
		{name: "db:duplicate", wantArgs: true},
		{name: "db:import", wantArgs: true, wantFlag: "create"},
		{name: "db:export", wantArgs: true},
		{name: "db:query", wantArgs: true, wantFlag: "limit"},
		{name: "db:connection", wantArgs: true},
	} {
		assertCmd(t, tc)
	}
}

func TestSplitTarget(t *testing.T) {
	site := dbTarget{Engine: "mysql", Name: "from_env"}
	fromSite := func() (dbTarget, error) { return site, nil }
	cases := []struct {
		pos      []string
		want     dbTarget
		wantRest []string
	}{
		{[]string{"pgsql", "app", "dump.sql"}, dbTarget{"postgres", "app"}, []string{"dump.sql"}},
		{[]string{"MariaDB", "shop"}, dbTarget{"mysql", "shop"}, nil},
		{[]string{"sqlite", "blog", "SELECT 1"}, dbTarget{"sqlite", "blog"}, []string{"SELECT 1"}},
		{[]string{"dump.sql"}, site, []string{"dump.sql"}},
		{nil, site, nil},
	}
	for _, tc := range cases {
		got, rest, err := splitTarget(tc.pos, fromSite)
		if err != nil {
			t.Fatalf("%q: %v", tc.pos, err)
		}
		if got != tc.want || strings.Join(rest, "|") != strings.Join(tc.wantRest, "|") {
			t.Errorf("%q: got %v rest %q, want %v rest %q", tc.pos, got, rest, tc.want, tc.wantRest)
		}
	}

	if _, _, err := splitTarget([]string{"mysql"}, fromSite); err == nil {
		t.Error("engine without a name: want error")
	}
	noSite := errors.New("no site found")
	if _, _, err := splitTarget([]string{"dump.sql"}, func() (dbTarget, error) { return dbTarget{}, noSite }); !errors.Is(err, noSite) {
		t.Errorf("no site in cwd: got %v, want %v", err, noSite)
	}
}

func TestParseInterspersed_FlagsInAnyPosition(t *testing.T) {
	fs := newTestFlagSet()
	force := fs.Bool("force", false, "")
	pos, err := parseInterspersed(fs, []string{"mysql", "--force", "shop"})
	if err != nil || !*force || strings.Join(pos, " ") != "mysql shop" {
		t.Fatalf("got pos %q force %v err %v", pos, *force, err)
	}

	fs = newTestFlagSet()
	limit := fs.Int("limit", 500, "")
	pos, err = parseInterspersed(fs, []string{"--limit=5", "--", "-- comment\nSELECT 1"})
	if err != nil || *limit != 5 || len(pos) != 1 || pos[0] != "-- comment\nSELECT 1" {
		t.Fatalf("after --: got pos %q limit %d err %v", pos, *limit, err)
	}

	if _, err := parseInterspersed(newTestFlagSet(), []string{"shop", "--nope"}); err == nil {
		t.Error("unknown flag: want error")
	}
}

func TestConnectionFromEnv(t *testing.T) {
	root := filepath.FromSlash("/sites/blog")
	cases := []struct {
		name string
		env  map[string]string
		want dbConnection
	}{
		{
			name: "mysql with every key",
			env:  map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "localhost", "DB_PORT": "3307", "DB_DATABASE": "shop", "DB_USERNAME": "app", "DB_PASSWORD": "p@ss"},
			want: dbConnection{Driver: "mysql", Engine: "mysql", Name: "shop", Host: "localhost", Port: "3307", Database: "shop", Username: "app", Password: "p@ss", URL: "mysql://app:p%40ss@localhost:3307/shop"},
		},
		{
			name: "pgsql takes Laravel defaults",
			env:  map[string]string{"DB_CONNECTION": "pgsql", "DB_DATABASE": "app"},
			want: dbConnection{Driver: "pgsql", Engine: "postgres", Name: "app", Host: "127.0.0.1", Port: "5432", Database: "app", Username: "root", URL: "postgresql://root@127.0.0.1:5432/app"},
		},
		{
			name: "mariadb uses the MySQL engine",
			env:  map[string]string{"DB_CONNECTION": "mariadb", "DB_DATABASE": "shop"},
			want: dbConnection{Driver: "mariadb", Engine: "mysql", Name: "shop", Host: "127.0.0.1", Port: "3306", Database: "shop", Username: "root", URL: "mysql://root@127.0.0.1:3306/shop"},
		},
		{
			name: "sqlite default file is named after the site directory",
			env:  map[string]string{"DB_CONNECTION": "sqlite"},
			want: dbConnection{Driver: "sqlite", Engine: "sqlite", Name: "blog", Database: filepath.Join(root, "database", "database.sqlite"), URL: "sqlite://" + filepath.ToSlash(filepath.Join(root, "database", "database.sqlite"))},
		},
		{
			name: "sqlite relative file is named after the file",
			env:  map[string]string{"DB_CONNECTION": "sqlite", "DB_DATABASE": "database/testing.sqlite"},
			want: dbConnection{Driver: "sqlite", Engine: "sqlite", Name: "testing", Database: filepath.Join(root, "database", "testing.sqlite"), URL: "sqlite://" + filepath.ToSlash(filepath.Join(root, "database", "testing.sqlite"))},
		},
	}
	for _, tc := range cases {
		got, err := connectionFromEnv(tc.env, root)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}

	for _, env := range []map[string]string{{}, {"DB_CONNECTION": "sqlsrv"}} {
		if _, err := connectionFromEnv(env, root); err == nil {
			t.Errorf("env %v: want error", env)
		}
	}
}

func TestParseDotenv(t *testing.T) {
	env := parseDotenv(strings.Join([]string{
		"# DB_CONNECTION=pgsql",
		"APP_NAME=\"My App\"",
		"export DB_CONNECTION=mysql",
		"DB_PASSWORD='s3cr#t' # quoted hash stays",
		"DB_DATABASE=shop # trailing comment",
		"DB_HOST = 127.0.0.1\r",
		"EMPTY=",
	}, "\n"))
	want := map[string]string{
		"APP_NAME":      "My App",
		"DB_CONNECTION": "mysql",
		"DB_PASSWORD":   "s3cr#t",
		"DB_DATABASE":   "shop",
		"DB_HOST":       "127.0.0.1",
		"EMPTY":         "",
	}
	if len(env) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(env), env, len(want))
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}

func TestCellText(t *testing.T) {
	for raw, want := range map[string]string{
		`"plain"`:          "plain",
		`"two\nlines"`:     `two\nlines`,
		`9007199254740993`: "9007199254740993",
		`{"a":1}`:          `{"a":1}`,
		`true`:             "true",
	} {
		if got := cellText([]byte(raw)); got != want {
			t.Errorf("cellText(%s) = %q, want %q", raw, got, want)
		}
	}
	if got := stripANSI(cellText([]byte("null"))); got != "NULL" {
		t.Errorf("cellText(null) = %q, want NULL", got)
	}
}

// TestImportDatabase_StreamsPastClientTimeout proves an import is not cut off
// by the 15 s timeout of the shared client.
func TestImportDatabase_StreamsPastClientTimeout(t *testing.T) {
	const dump = "CREATE TABLE t (id INT);\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/databases/mysql/databases/my shop/import" || r.URL.Query().Get("create") != "1" || string(body) != dump {
			http.Error(w, `{"error":"unexpected request"}`, http.StatusBadRequest)
			return
		}
		time.Sleep(150 * time.Millisecond)
		w.Write([]byte(`{"status":"ok","bytes":25,"created":true}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: &http.Client{Timeout: 50 * time.Millisecond}}
	res, err := c.ImportDatabase("mysql", "my shop", strings.NewReader(dump), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Bytes != 25 || !res.Created {
		t.Errorf("got %+v, want 25 bytes and created", res)
	}
}

func TestImportDatabase_ReturnsEngineError(t *testing.T) {
	const msg = `ERROR 1064 (42000) at line 1: You have an error in your SQL syntax`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"` + msg + `"}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	_, err := c.ImportDatabase("mysql", "shop", strings.NewReader("SELEC 1;"), false)
	if err == nil || err.Error() != msg {
		t.Fatalf("got %v, want the engine message %q", err, msg)
	}
}

func TestExportToFile_WritesDump(t *testing.T) {
	const dump = "CREATE TABLE people (id INT);\nINSERT INTO people VALUES (1);\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/databases/postgres/databases/app/export" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(dump))
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "app.sql")
	c := &Client{base: srv.URL, http: srv.Client()}
	n, err := exportToFile(c, dbTarget{"postgres", "app"}, file, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if string(got) != dump || n != int64(len(dump)) {
		t.Errorf("file %q (%d bytes reported), want %q", got, n, dump)
	}
	assertOnlyFile(t, dir, "app.sql")
}

// TestExportToFile_AbortedDumpKeepsOldFile covers the daemon aborting the
// connection when mysqldump fails after the first bytes were sent.
func TestExportToFile_AbortedDumpKeepsOldFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("CREATE TABLE half"))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "shop.sql")
	if err := os.WriteFile(file, []byte("previous dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Client{base: srv.URL, http: srv.Client()}
	if _, err := exportToFile(c, dbTarget{"mysql", "shop"}, file, io.Discard); err == nil {
		t.Fatal("aborted export: want error")
	}
	if got, _ := os.ReadFile(file); string(got) != "previous dump" {
		t.Errorf("existing file changed to %q", got)
	}
	assertOnlyFile(t, dir, "shop.sql")
}

func TestExportDatabase_ReturnsErrorBeforeStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"mysqldump: Got error: 1049: Unknown database 'nope'"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := &Client{base: srv.URL, http: srv.Client()}
	_, err := exportToFile(c, dbTarget{"mysql", "nope"}, filepath.Join(dir, "nope.sql"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "Unknown database") {
		t.Fatalf("got %v, want the engine error", err)
	}
	assertOnlyFile(t, dir, "")
}

func TestDBDrop_NeedsForceWhenStdinIsNotATerminal(t *testing.T) {
	var drops []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			drops = append(drops, r.URL.Path)
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	orig := os.Stdin
	os.Stdin = stdin
	defer func() { os.Stdin = orig }()

	c := &Client{base: srv.URL, http: srv.Client()}
	drop := Find("db:drop").Handler
	if err := drop(c, []string{"mysql", "shop"}, true); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("without --force: got %v, want a --force error", err)
	}
	if len(drops) != 0 {
		t.Fatalf("without --force the database was dropped: %v", drops)
	}
	if err := drop(c, []string{"mysql", "--force", "shop"}, true); err != nil {
		t.Fatal(err)
	}
	if len(drops) != 1 || drops[0] != "/api/databases/mysql/databases/shop" {
		t.Errorf("with --force: DELETE calls %v, want one for mysql/shop", drops)
	}
}

func newTestFlagSet() *flag.FlagSet { return flag.NewFlagSet("test", flag.ContinueOnError) }

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{}
	if name != "" {
		want = []string{name}
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("%s holds %q, want %q (no partial files)", dir, names, want)
	}
}
