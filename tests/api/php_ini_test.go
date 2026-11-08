//go:build integration

package apitest

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/php"
)

type phpSettingsResp struct {
	MemoryLimit      string   `json:"memory_limit"`
	MaxExecutionTime string   `json:"max_execution_time"`
	Scope            string   `json:"scope"`
	Overrides        []string `json:"overrides"`
}

type phpIniDirective struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

func phpTestServerRoot() string {
	if r := os.Getenv("DEVCTL_SERVER_ROOT"); r != "" {
		return r
	}
	return "/home/testuser/ddev/sites/server"
}

// firstPHPVersion skips the test when no PHP version (or stub) is installed.
func firstPHPVersion(t *testing.T) string {
	t.Helper()
	versions := decodeJSON[[]struct {
		Version string `json:"version"`
	}](t, httpGet(t, "/api/php/versions"))
	if len(versions) == 0 {
		t.Skip("no PHP version installed")
	}
	return versions[0].Version
}

// restorePHPIniAfter puts the directive table back to its state at call time.
func restorePHPIniAfter(t *testing.T) {
	t.Helper()
	before := decodeJSON[[]phpIniDirective](t, httpGet(t, "/api/php/ini"))
	t.Cleanup(func() {
		after := decodeJSON[[]phpIniDirective](t, httpGet(t, "/api/php/ini"))
		for _, d := range after {
			if !slices.Contains(before, d) {
				httpDelete(t, "/api/php/ini/"+d.Scope+"/"+d.Key)
			}
		}
		for _, d := range before {
			if !slices.Contains(after, d) {
				httpPut(t, "/api/php/ini/"+d.Scope+"/"+d.Key, map[string]string{"value": d.Value})
			}
		}
	})
}

func TestPHPSettings_VersionScopeOverridesGlobal(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)

	if body, status := httpPut(t, "/api/php/settings", map[string]string{"memory_limit": "300M"}); status != http.StatusOK {
		t.Fatalf("PUT global: %d %s", status, body)
	}
	body, status := httpPut(t, "/api/php/settings?version="+ver, map[string]string{"memory_limit": "400M"})
	if status != http.StatusOK {
		t.Fatalf("PUT version: %d %s", status, body)
	}
	v := decodeJSON[phpSettingsResp](t, body)
	if v.Scope != ver || v.MemoryLimit != "400M" || !slices.Contains(v.Overrides, "memory_limit") {
		t.Errorf("PUT version response = %+v", v)
	}

	global := decodeJSON[phpSettingsResp](t, httpGet(t, "/api/php/settings"))
	if global.Scope != "all" || global.MemoryLimit != "300M" {
		t.Errorf("global = %+v, want memory_limit 300M", global)
	}

	if _, status := httpDelete(t, "/api/php/ini/"+ver+"/memory_limit"); status != http.StatusNoContent {
		t.Fatalf("DELETE version override: %d", status)
	}
	v = decodeJSON[phpSettingsResp](t, httpGet(t, "/api/php/settings?version="+ver))
	if v.MemoryLimit != "300M" || slices.Contains(v.Overrides, "memory_limit") {
		t.Errorf("after unset = %+v, want memory_limit 300M from all versions", v)
	}
}

func TestPHPSettings_PutOnlyStoresGivenKeys(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)

	if _, status := httpPut(t, "/api/php/settings?version="+ver, map[string]string{"max_execution_time": "45"}); status != http.StatusOK {
		t.Fatalf("PUT: %d", status)
	}
	ds := decodeJSON[[]phpIniDirective](t, httpGet(t, "/api/php/ini?scope="+ver))
	for _, d := range ds {
		if d.Key == "memory_limit" || d.Key == "post_max_size" || d.Key == "upload_max_filesize" {
			t.Errorf("unexpected directive stored: %+v", d)
		}
	}
}

func TestPHPSettings_RejectsUnknownAndInvalidVersion(t *testing.T) {
	httpGetStatus(t, "/api/php/settings?version=9.9", http.StatusNotFound)
	httpGetStatus(t, "/api/php/settings?version=latest", http.StatusBadRequest)
	if _, status := httpPut(t, "/api/php/settings?version=9.9", map[string]string{"memory_limit": "1G"}); status != http.StatusNotFound {
		t.Errorf("PUT unknown version: %d, want 404", status)
	}
}

func TestPHPIni_SetListUnset_WritesManagedBlock(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)
	iniPath := php.PHPIniPath(ver, phpTestServerRoot())

	body, status := httpPut(t, "/api/php/ini/all/date.timezone", map[string]string{"value": "Europe/Berlin"})
	if status != http.StatusOK {
		t.Fatalf("PUT: %d %s", status, body)
	}
	ds := decodeJSON[[]phpIniDirective](t, httpGet(t, "/api/php/ini?scope=all"))
	if !slices.Contains(ds, phpIniDirective{Scope: "all", Key: "date.timezone", Value: "Europe/Berlin"}) {
		t.Errorf("list = %+v, want date.timezone", ds)
	}
	ini := mustReadFile(t, iniPath)
	marker := strings.Index(ini, "devctl managed directives")
	line := strings.Index(ini, "date.timezone = Europe/Berlin")
	if marker < 0 || line < marker {
		t.Errorf("php.ini has no managed block with date.timezone:\n%s", ini)
	}

	if _, status := httpDelete(t, "/api/php/ini/all/date.timezone"); status != http.StatusNoContent {
		t.Fatalf("DELETE: %d", status)
	}
	if strings.Contains(mustReadFile(t, iniPath), "date.timezone = Europe/Berlin") {
		t.Error("directive still in php.ini after unset")
	}
}

func TestPHPIni_KeepsUserEditsOutsideBlock(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)
	iniPath := php.PHPIniPath(ver, phpTestServerRoot())

	const userLine = "; devctl-test user edit"
	orig := mustReadFile(t, iniPath)
	info, err := os.Stat(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iniPath, []byte(orig+userLine+"\n"), info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cur := mustReadFile(t, iniPath)
		_ = os.WriteFile(iniPath, []byte(strings.Replace(cur, userLine+"\n", "", 1)), info.Mode().Perm())
	})

	if _, status := httpPut(t, "/api/php/ini/"+ver+"/display_errors", map[string]string{"value": "On"}); status != http.StatusOK {
		t.Fatalf("PUT: %d", status)
	}
	ini := mustReadFile(t, iniPath)
	if !strings.Contains(ini, userLine) {
		t.Errorf("user edit lost:\n%s", ini)
	}
	if strings.Index(ini, "display_errors = On") < strings.Index(ini, userLine) {
		t.Errorf("managed block must come after user lines:\n%s", ini)
	}
}

func TestPHPIni_RejectsBadInput(t *testing.T) {
	restorePHPIniAfter(t)
	cases := []struct {
		name, path string
		body       any
		want       int
	}{
		{"bad key", "/api/php/ini/all/bad%20key", map[string]string{"value": "1"}, http.StatusBadRequest},
		{"multi-line value", "/api/php/ini/all/memory_limit", map[string]string{"value": "1G\nauto_prepend_file=/tmp/x"}, http.StatusBadRequest},
		{"missing value", "/api/php/ini/all/memory_limit", map[string]string{}, http.StatusBadRequest},
		{"bad scope", "/api/php/ini/latest/memory_limit", map[string]string{"value": "1G"}, http.StatusBadRequest},
		{"unknown version", "/api/php/ini/9.9/memory_limit", map[string]string{"value": "1G"}, http.StatusNotFound},
	}
	for _, tc := range cases {
		if body, status := httpPut(t, tc.path, tc.body); status != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", tc.name, status, tc.want, body)
		}
	}
	if _, status := httpDelete(t, "/api/php/ini/all/devctl_test_never_set"); status != http.StatusNotFound {
		t.Errorf("DELETE unset key: %d, want 404", status)
	}
}

func TestServiceSettingsPHPFPM_PutAppliesToAllVersions(t *testing.T) {
	ver := firstPHPVersion(t)
	restorePHPIniAfter(t)

	if body, status := httpPut(t, "/api/services/php-fpm-"+ver+"/settings", map[string]string{"max_execution_time": "77"}); status != http.StatusOK {
		t.Fatalf("PUT: %d %s", status, body)
	}
	global := decodeJSON[phpSettingsResp](t, httpGet(t, "/api/php/settings"))
	if global.MaxExecutionTime != "77" {
		t.Errorf("global max_execution_time = %q, want 77", global.MaxExecutionTime)
	}
}
