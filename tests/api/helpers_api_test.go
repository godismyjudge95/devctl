//go:build integration

package apitest

import (
	"net/http"
	"testing"
)

type HelperState struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	Homepage        string   `json:"homepage"`
	Aliases         []string `json:"aliases"`
	Installed       bool     `json:"installed"`
	Default         bool     `json:"default"`
	Version         string   `json:"version"`
	LatestVersion   string   `json:"latest_version"`
	UpdateAvailable bool     `json:"update_available"`
}

func TestGetHelpers_StatusOK(t *testing.T) {
	httpGet(t, "/api/helpers")
}

func TestGetHelpers_IncludesCatalog(t *testing.T) {
	body := httpGet(t, "/api/helpers")
	helpers := decodeJSON[[]HelperState](t, body)
	if len(helpers) < 5 {
		t.Fatalf("expected at least sqlite3, mago, phpantom_lsp, fnm, yq; got %d", len(helpers))
	}
	byID := map[string]HelperState{}
	for _, h := range helpers {
		if h.ID == "" || h.Label == "" {
			t.Errorf("helper missing id/label: %+v", h)
		}
		byID[h.ID] = h
	}
	for _, id := range []string{"sqlite3", "mago", "phpantom_lsp", "fnm", "yq"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing helper %s", id)
		}
	}
	sqlite3 := byID["sqlite3"]
	if !sqlite3.Default {
		t.Error("sqlite3 should be marked default")
	}
	if !sqlite3.Installed {
		t.Error("sqlite3 should be installed by default")
	}
	if byID["mago"].Default {
		t.Error("mago should be opt-in")
	}
}

func TestHelperUninstall_RejectsDefault(t *testing.T) {
	_, status := httpDelete(t, "/api/helpers/sqlite3")
	if status != http.StatusForbidden {
		t.Fatalf("uninstall sqlite3: status %d, want 403", status)
	}
}

func TestHelperInstall_Unknown(t *testing.T) {
	_, status := httpPost(t, "/api/helpers/not-a-tool/install", nil)
	if status != http.StatusNotFound {
		t.Fatalf("install unknown: status %d, want 404", status)
	}
}

func TestHelperUpdate_NotInstalled(t *testing.T) {
	body := httpGet(t, "/api/helpers")
	helpers := decodeJSON[[]HelperState](t, body)
	var target string
	for _, h := range helpers {
		if !h.Installed && h.ID != "sqlite3" {
			target = h.ID
			break
		}
	}
	if target == "" {
		t.Skip("no uninstalled helper to test update conflict")
	}
	_, status := httpPost(t, "/api/helpers/"+target+"/update", nil)
	if status != http.StatusConflict && status != http.StatusOK {
		// SSE endpoints set status 200 then stream; conflict is returned before SSE.
		t.Fatalf("update uninstalled %s: status %d, want 409", target, status)
	}
	if status == http.StatusOK {
		t.Fatalf("update of uninstalled %s should be 409", target)
	}
}

func TestHelperUpdateAvailable_Inject(t *testing.T) {
	injectBody, injectStatus := httpPost(t, "/_testing/helpers/sqlite3/latest-version", map[string]string{
		"version": "v9999.0.0",
	})
	if injectStatus != http.StatusOK {
		t.Fatalf("inject: %d %s", injectStatus, injectBody)
	}
	body := httpGet(t, "/api/helpers")
	helpers := decodeJSON[[]HelperState](t, body)
	for _, h := range helpers {
		if h.ID == "sqlite3" {
			if !h.UpdateAvailable {
				t.Fatalf("expected sqlite3 update_available after inject, got %+v", h)
			}
			return
		}
	}
	t.Fatal("sqlite3 missing from helpers list")
}
