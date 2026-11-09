package api

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestPgIdempotentServers_RewritesServersAndMappingsOutsideCopy(t *testing.T) {
	in := strings.Join([]string{
		"CREATE EXTENSION IF NOT EXISTS pg_clickhouse WITH SCHEMA public;",
		"CREATE SERVER clickhouse FOREIGN DATA WRAPPER clickhouse_fdw OPTIONS (",
		"    driver 'http'",
		");",
		"CREATE USER MAPPING FOR root SERVER clickhouse OPTIONS (",
		"    \"user\" 'default'",
		");",
		"CREATE SERVER IF NOT EXISTS already FOREIGN DATA WRAPPER x;",
		"COPY public.notes (id, body) FROM stdin;",
		"1\tCREATE SERVER inside a row",
		"CREATE USER MAPPING FOR row text",
		`\.`,
		"CREATE SERVER after FOREIGN DATA WRAPPER x;",
		"",
	}, "\n")
	want := strings.Join([]string{
		"CREATE EXTENSION IF NOT EXISTS pg_clickhouse WITH SCHEMA public;",
		"CREATE SERVER IF NOT EXISTS clickhouse FOREIGN DATA WRAPPER clickhouse_fdw OPTIONS (",
		"    driver 'http'",
		");",
		"CREATE USER MAPPING IF NOT EXISTS FOR root SERVER clickhouse OPTIONS (",
		"    \"user\" 'default'",
		");",
		"CREATE SERVER IF NOT EXISTS already FOREIGN DATA WRAPPER x;",
		"COPY public.notes (id, body) FROM stdin;",
		"1\tCREATE SERVER inside a row",
		"CREATE USER MAPPING FOR row text",
		`\.`,
		"CREATE SERVER IF NOT EXISTS after FOREIGN DATA WRAPPER x;",
		"",
	}, "\n")
	// OneByteReader proves lines that arrive in pieces are still rewritten.
	got, err := io.ReadAll(iotest.OneByteReader(newPgIdempotentServers(iotest.OneByteReader(strings.NewReader(in)))))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPgIdempotentServers_PassesReadErrorsThrough(t *testing.T) {
	_, err := io.ReadAll(newPgIdempotentServers(iotest.TimeoutReader(strings.NewReader("CREATE SERVER x FOREIGN DATA WRAPPER y;\nSELECT 1;\n"))))
	if err == nil {
		t.Fatal("expected the source error")
	}
}
