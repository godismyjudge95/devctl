package database

import (
	"strings"
	"testing"
)

func TestFormatExportCSVJSONSQL(t *testing.T) {
	cols := []Column{{Name: "id"}, {Name: "name"}}
	rows := [][]any{{int64(1), "Ada"}, {int64(2), nil}}

	csvOut, err := FormatExport("csv", "users", "sqlite", cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(csvOut.Body), "id,name") {
		t.Fatalf("csv header: %s", csvOut.Body)
	}
	if csvOut.Filename != "users.csv" {
		t.Fatalf("filename %s", csvOut.Filename)
	}

	js, err := FormatExport("json", "users", "sqlite", cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js.Body), `"name": "Ada"`) {
		t.Fatalf("json: %s", js.Body)
	}

	sqlOut, err := FormatExport("sql", "users", "sqlite", cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	body := string(sqlOut.Body)
	if !strings.Contains(body, `INSERT INTO "users"`) {
		t.Fatalf("sql: %s", body)
	}
	if !strings.Contains(body, "NULL") {
		t.Fatalf("expected NULL literal: %s", body)
	}

	if _, err := FormatExport("xml", "users", "sqlite", cols, rows); err == nil {
		t.Fatal("expected unsupported format error")
	}
}
