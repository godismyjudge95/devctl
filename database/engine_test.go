package database

import (
	"os"
	"path/filepath"
	"testing"
)

func openTestSQLite(t *testing.T) Engine {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.sqlite")
	eng, err := Open(Config{
		Kind:  "sqlite",
		Files: []SQLiteFile{{Name: "app", Path: path}},
	}, NewPool())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestSQLiteCreateTableInsertSelectUpdateDelete(t *testing.T) {
	e := openTestSQLite(t)

	dbs, err := e.ListDatabases()
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 1 || dbs[0].Name != "app" {
		t.Fatalf("databases = %+v", dbs)
	}

	err = e.CreateTable("app", "", "users", []ColumnDef{
		{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
		{Name: "name", Type: "TEXT", Nullable: false},
		{Name: "email", Type: "TEXT", Nullable: true},
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	tables, err := e.ListTables("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != "users" {
		t.Fatalf("tables = %+v", tables)
	}

	if err := e.InsertRow("app", "", "users", map[string]any{"name": "Ada", "email": "ada@example.test"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := e.InsertRow("app", "", "users", map[string]any{"name": "Alan", "email": nil}); err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	page, err := e.Rows(RowsQuery{Database: "app", Table: "users", Limit: 50, Sort: "id", Dir: "asc"})
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d", page.Total)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("row count = %d", len(page.Rows))
	}
	if len(page.PrimaryKey) != 1 || page.PrimaryKey[0] != "id" {
		t.Fatalf("pk = %+v", page.PrimaryKey)
	}

	nameIdx := -1
	idIdx := -1
	emailIdx := -1
	for i, c := range page.Columns {
		switch c.Name {
		case "name":
			nameIdx = i
		case "id":
			idIdx = i
		case "email":
			emailIdx = i
		}
	}
	if nameIdx < 0 || idIdx < 0 || emailIdx < 0 {
		t.Fatalf("columns = %+v", page.Columns)
	}
	if page.Rows[0][nameIdx] != "Ada" {
		t.Fatalf("first name = %#v", page.Rows[0][nameIdx])
	}
	if page.Rows[1][emailIdx] != nil {
		t.Fatalf("second email should be null, got %#v", page.Rows[1][emailIdx])
	}

	id := page.Rows[0][idIdx]
	if err := e.UpdateRow("app", "", "users", map[string]any{"id": id}, map[string]any{"name": "Ada Lovelace"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	page, err = e.Rows(RowsQuery{Database: "app", Table: "users", Where: "name LIKE '%Lovelace%'"})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("filtered total = %d", page.Total)
	}

	if err := e.DeleteRows("app", "", "users", []map[string]any{{"id": id}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	page, err = e.Rows(RowsQuery{Database: "app", Table: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("after delete total = %d", page.Total)
	}

	st, err := e.Structure("app", "", "users")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Columns) < 3 {
		t.Fatalf("structure columns = %+v", st.Columns)
	}
	if st.CreateSQL == "" {
		t.Fatal("expected create sql")
	}

	qr, err := e.ExecQuery("app", "SELECT name FROM users", 100)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(qr.Rows) != 1 {
		t.Fatalf("query rows = %d", len(qr.Rows))
	}

	if err := e.TruncateTable("app", "", "users"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	page, err = e.Rows(RowsQuery{Database: "app", Table: "users"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("after truncate total = %d", page.Total)
	}

	if err := e.DropTable("app", "", "users"); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	tables, err = e.ListTables("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 0 {
		t.Fatalf("tables after drop = %+v", tables)
	}
}

func TestSQLiteRejectsBadWhere(t *testing.T) {
	e := openTestSQLite(t)
	_ = e.CreateTable("app", "", "t", []ColumnDef{{Name: "id", Type: "INTEGER", PrimaryKey: true}})
	_, err := e.Rows(RowsQuery{Database: "app", Table: "t", Where: "1=1; DROP TABLE t"})
	if err == nil {
		t.Fatal("expected error for semicolon in WHERE")
	}
}

func TestSQLiteRejectsUnknownSort(t *testing.T) {
	e := openTestSQLite(t)
	_ = e.CreateTable("app", "", "t", []ColumnDef{{Name: "id", Type: "INTEGER", PrimaryKey: true}})
	_, err := e.Rows(RowsQuery{Database: "app", Table: "t", Sort: "not_a_column"})
	if err == nil {
		t.Fatal("expected unknown sort error")
	}
}

func TestSQLiteExecDML(t *testing.T) {
	e := openTestSQLite(t)
	_ = e.CreateTable("app", "", "t", []ColumnDef{
		{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
		{Name: "n", Type: "INTEGER"},
	})
	res, err := e.ExecQuery("app", "INSERT INTO t (n) VALUES (1), (2), (3)", 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowsAffected != 3 {
		t.Fatalf("rows affected = %d", res.RowsAffected)
	}
}

func TestDiscoverSQLite(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "blog")
	if err := os.MkdirAll(filepath.Join(site, "database"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(site, "database", "database.sqlite")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	// server dir should be skipped
	_ = os.MkdirAll(filepath.Join(root, "server"), 0755)
	_ = os.WriteFile(filepath.Join(root, "server", "database.sqlite"), []byte{}, 0644)

	found := DiscoverSQLite(root)
	if len(found) != 1 || found[0].Name != "blog" {
		t.Fatalf("found = %+v", found)
	}
}

func TestSQLiteRenameDuplicateTableAndColumn(t *testing.T) {
	e := openTestSQLite(t)
	if err := e.CreateTable("app", "", "users", []ColumnDef{
		{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
		{Name: "name", Type: "TEXT"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.InsertRow("app", "", "users", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := e.DuplicateTable("app", "", "users", "users_copy"); err != nil {
		t.Fatalf("dup table: %v", err)
	}
	if err := e.RenameTable("app", "", "users_copy", "people"); err != nil {
		t.Fatalf("rename table: %v", err)
	}
	if err := e.AddColumn("app", "", "people", ColumnDef{Name: "bio", Type: "TEXT", Nullable: true}); err != nil {
		t.Fatalf("add col: %v", err)
	}
	if err := e.RenameColumn("app", "", "people", "bio", "about"); err != nil {
		t.Fatalf("rename col: %v", err)
	}
	st, err := e.Structure("app", "", "people")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range st.Columns {
		if c.Name == "about" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected about column: %+v", st.Columns)
	}
	if err := e.DropColumn("app", "", "people", "about"); err != nil {
		t.Fatalf("drop col: %v", err)
	}
	exp, err := e.Export(RowsQuery{Database: "app", Table: "people"}, "json")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(exp.Body) == 0 {
		t.Fatal("empty export")
	}
}

func TestSQLiteDuplicateDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.sqlite")
	eng, err := Open(Config{Kind: "sqlite", Files: []SQLiteFile{{Name: "app", Path: path}}}, NewPool())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.CreateTable("app", "", "t", []ColumnDef{{Name: "id", Type: "INTEGER", PrimaryKey: true}}); err != nil {
		t.Fatal(err)
	}
	if err := eng.DuplicateDatabase("app", "app2"); err != nil {
		t.Fatalf("dup db: %v", err)
	}
	dbs, err := eng.ListDatabases()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range dbs {
		names[d.Name] = true
	}
	if !names["app2"] {
		t.Fatalf("missing app2: %+v", dbs)
	}
}

func TestUnknownKind(t *testing.T) {
	_, err := Open(Config{Kind: "oracle"}, NewPool())
	if err == nil {
		t.Fatal("expected unknown kind error")
	}
}

func TestQuoteInjectionDoesNotBreakIdent(t *testing.T) {
	e := openTestSQLite(t)
	// Table name with quote should be rejected or safely quoted.
	err := e.CreateTable("app", "", `users"; drop`, []ColumnDef{{Name: "id", Type: "INTEGER", PrimaryKey: true}})
	if err != nil {
		// either validate or succeed as a weird name — must not drop other tables
		return
	}
	tables, err := e.ListTables("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("unexpected tables after quoted name: %+v", tables)
	}
}
