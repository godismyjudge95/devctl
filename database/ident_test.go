package database

import "testing"

func TestQuoteMySQL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"users", "`users`"},
		{"foo`bar", "`foo``bar`"},
		{"order", "`order`"},
	}
	for _, c := range cases {
		if got := quoteMySQL(c.in); got != c.want {
			t.Errorf("quoteMySQL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuotePG(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"users", `"users"`},
		{`foo"bar`, `"foo""bar"`},
	}
	for _, c := range cases {
		if got := quotePG(c.in); got != c.want {
			t.Errorf("quotePG(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQualify(t *testing.T) {
	t.Parallel()
	got := qualify("mysql", "app", "users")
	if got != "`app`.`users`" {
		t.Errorf("qualify mysql = %q", got)
	}
	got = qualify("postgres", "public", "users")
	if got != `"public"."users"` {
		t.Errorf("qualify postgres = %q", got)
	}
	got = qualify("sqlite", "", "users")
	if got != `"users"` {
		t.Errorf("qualify sqlite = %q", got)
	}
}

func TestValidateIdent(t *testing.T) {
	t.Parallel()
	if err := validateIdent(""); err == nil {
		t.Fatal("expected error for empty ident")
	}
	if err := validateIdent("users"); err != nil {
		t.Fatalf("users: %v", err)
	}
	if err := validateIdent("foo-bar.baz"); err != nil {
		t.Fatalf("odd but legal ident: %v", err)
	}
	if err := validateIdent("a\x00b"); err == nil {
		t.Fatal("expected error for NUL")
	}
}

func TestFirstSQLKeyword(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"SELECT 1":            "SELECT",
		"  insert into t":     "INSERT",
		"\nWith x AS (SELECT 1) SELECT * FROM x": "WITH",
		"show databases":      "SHOW",
		"":                    "",
	}
	for in, want := range cases {
		if got := firstSQLKeyword(in); got != want {
			t.Errorf("firstSQLKeyword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSystemDatabase(t *testing.T) {
	t.Parallel()
	if !isSystemDatabase("mysql", "information_schema") {
		t.Error("mysql information_schema should be system")
	}
	if isSystemDatabase("mysql", "app") {
		t.Error("mysql app should not be system")
	}
	if !isSystemDatabase("postgres", "template1") {
		t.Error("postgres template1 should be system")
	}
	if !isSystemDatabase("clickhouse", "INFORMATION_SCHEMA") {
		t.Error("clickhouse INFORMATION_SCHEMA should be system")
	}
}

func TestIsInternalTable(t *testing.T) {
	t.Parallel()
	if !isInternalTable("_timescaledb_internal", "chunk") {
		t.Fatal("timescaledb schema")
	}
	if !isInternalTable("public", "_hyper_1") {
		t.Fatal("underscore table")
	}
	if isInternalTable("public", "users") {
		t.Fatal("users is not internal")
	}
}

func TestSortTablesInternalLast(t *testing.T) {
	t.Parallel()
	tables := []TableInfo{
		{Name: "_hyper_1", Schema: "public", Internal: true},
		{Name: "users", Schema: "public"},
		{Name: "chunk", Schema: "_timescaledb_internal", Internal: true},
		{Name: "posts", Schema: "public"},
	}
	sortTables(tables)
	if tables[0].Name != "posts" && tables[0].Name != "users" {
		t.Fatalf("first should be user table, got %+v", tables[0])
	}
	if !tables[len(tables)-1].Internal && !isInternalTable(tables[len(tables)-1].Schema, tables[len(tables)-1].Name) {
		t.Fatalf("last should be internal, got %+v", tables[len(tables)-1])
	}
}

func TestSanitizeDir(t *testing.T) {
	t.Parallel()
	if sanitizeDir("desc") != "DESC" {
		t.Fatal("desc")
	}
	if sanitizeDir("nope") != "ASC" {
		t.Fatal("fallback")
	}
}
