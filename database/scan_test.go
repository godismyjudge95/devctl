package database

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeValue(t *testing.T) {
	t.Parallel()
	if normalizeValue(nil) != nil {
		t.Fatal("nil")
	}
	if normalizeValue(int64(7)) != int64(7) {
		t.Fatal("int64")
	}
	if normalizeValue("hi") != "hi" {
		t.Fatal("string")
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := normalizeValue(ts)
	if got != "2026-01-02T03:04:05Z" {
		t.Fatalf("time = %v", got)
	}
	raw := []byte(`{"a":1}`)
	nv := normalizeValue(raw)
	if _, ok := nv.(json.RawMessage); !ok {
		t.Fatalf("json bytes = %T", nv)
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()
	if clampLimit(0, 100, 1000) != 100 {
		t.Fatal("default")
	}
	if clampLimit(50, 100, 1000) != 50 {
		t.Fatal("ok")
	}
	if clampLimit(99999, 100, 1000) != 1000 {
		t.Fatal("cap")
	}
}

func TestJSONToSQLIntegralFloat(t *testing.T) {
	t.Parallel()
	if jsonToSQL(float64(3)) != int64(3) {
		t.Fatalf("got %v", jsonToSQL(float64(3)))
	}
	if jsonToSQL(1.5) != 1.5 {
		t.Fatal("keep float")
	}
}

func TestPlaceholders(t *testing.T) {
	t.Parallel()
	if placeholders(false, 3, 1) != "?, ?, ?" {
		t.Fatal("mysql")
	}
	if placeholders(true, 2, 1) != "$1, $2" {
		t.Fatal("pg")
	}
}
