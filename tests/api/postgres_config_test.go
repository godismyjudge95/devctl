//go:build integration

package apitest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPostgresConfig_RejectsUnknownFile(t *testing.T) {
	for _, file := range []string{"postmaster.pid", "my.cnf", "config.xml"} {
		body, status := httpGetRaw(t, "/api/services/postgres/config/"+file)
		if status != http.StatusBadRequest {
			t.Errorf("GET %s: want 400, got %d: %s", file, status, body)
		}
	}
}

func TestPostgresConfig_ReadsBothFiles(t *testing.T) {
	ensureInstalledRunning(t, "postgres", 20*time.Minute)

	want := map[string]string{
		"postgresql.conf": "max_connections",
		"pg_hba.conf":     "host",
	}
	for file, needle := range want {
		cfg := decodeJSON[map[string]string](t, httpGet(t, "/api/services/postgres/config/"+file))
		if !strings.Contains(cfg["content"], needle) {
			t.Errorf("%s: expected %q in content", file, needle)
		}
	}
}

// TestPostgresConfig_SaveRestartAppliesSetting writes a work_mem line to
// postgresql.conf through the config endpoint and checks the restarted server
// reports it.
func TestPostgresConfig_SaveRestartAppliesSetting(t *testing.T) {
	ensureInstalledRunning(t, "postgres", 20*time.Minute)

	const path = "/api/services/postgres/config/postgresql.conf"
	original := decodeJSON[map[string]string](t, httpGet(t, path))["content"]
	t.Cleanup(func() {
		if body, status := httpPut(t, path, map[string]string{"content": original}); status != http.StatusNoContent {
			t.Errorf("restore postgresql.conf: %d %s", status, body)
		}
		pollServiceStatus(t, "postgres", "running", 2*time.Minute)
	})

	updated := original + "\nwork_mem = '7777kB'\n"
	if body, status := httpPut(t, path, map[string]string{"content": updated}); status != http.StatusNoContent {
		t.Fatalf("PUT postgresql.conf: %d %s", status, body)
	}
	pollServiceStatus(t, "postgres", "running", 2*time.Minute)

	saved := decodeJSON[map[string]string](t, httpGet(t, path))["content"]
	if saved != updated {
		t.Fatalf("postgresql.conf not saved as sent")
	}

	deadline := time.Now().Add(time.Minute)
	for {
		body, status := httpPost(t, "/api/databases/postgres/query", map[string]any{
			"database": "postgres",
			"sql":      "SELECT current_setting('work_mem')",
			"limit":    1,
		})
		if status == http.StatusOK {
			res := decodeJSON[struct {
				Rows [][]any `json:"rows"`
			}](t, body)
			if len(res.Rows) == 1 && len(res.Rows[0]) == 1 && fmt.Sprint(res.Rows[0][0]) == "7777kB" {
				return
			}
			t.Logf("work_mem not applied yet: %s", body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("work_mem never became 7777kB; last %d: %s", status, body)
		}
		time.Sleep(2 * time.Second)
	}
}
