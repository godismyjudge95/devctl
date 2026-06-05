//go:build integration

package apitest

import (
	"net/http"
	"testing"
)

type meilisearchSettingsResponse struct {
	Env  string `json:"env"`
	Args string `json:"args"`
}

func TestServiceSettings_Meilisearch_RoundTrip(t *testing.T) {
	const path = "/api/services/meilisearch/settings"

	originalBody := httpGet(t, path)
	original := decodeJSON[meilisearchSettingsResponse](t, originalBody)
	defer httpPut(t, path, original) //nolint:errcheck

	input := meilisearchSettingsResponse{
		Env:  "MEILI_EXPERIMENTAL_ALLOWED_IP_NETWORKS=any\nMEILI_LOG_LEVEL=INFO",
		Args: `--experimental-allowed-ip-networks any --log-level "INFO"`,
	}

	putBody, putStatus := httpPut(t, path, input)
	if putStatus != http.StatusOK {
		t.Fatalf("PUT %s: expected status 200, got %d: %s", path, putStatus, string(putBody))
	}

	var putResp map[string]string
	putResp = decodeJSON[map[string]string](t, putBody)
	if putResp["status"] != "ok" {
		t.Fatalf("PUT %s: status = %q, want %q", path, putResp["status"], "ok")
	}

	gotBody := httpGet(t, path)
	got := decodeJSON[meilisearchSettingsResponse](t, gotBody)
	if got.Env != input.Env {
		t.Errorf("GET %s: env = %q, want %q", path, got.Env, input.Env)
	}
	if got.Args != input.Args {
		t.Errorf("GET %s: args = %q, want %q", path, got.Args, input.Args)
	}
}

func TestServiceSettings_Meilisearch_InvalidEnvRejected(t *testing.T) {
	body, status := httpPut(t, "/api/services/meilisearch/settings", map[string]string{
		"env":  "NOT_VALID",
		"args": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid env: expected status 400, got %d: %s", status, string(body))
	}
}

func TestServiceSettings_Meilisearch_InvalidArgsRejected(t *testing.T) {
	body, status := httpPut(t, "/api/services/meilisearch/settings", map[string]string{
		"env":  "",
		"args": `--bad "unterminated`,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid args: expected status 400, got %d: %s", status, string(body))
	}
}
