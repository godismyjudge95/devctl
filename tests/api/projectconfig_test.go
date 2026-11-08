//go:build integration

package apitest

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type projectConfigResp struct {
	Files    []string `json:"files"`
	Managed  []string `json:"managed"`
	Error    string   `json:"error"`
	Services []struct {
		ID     string `json:"id"`
		Label  string `json:"label"`
		Status string `json:"status"`
	} `json:"services"`
}

type configSite struct {
	ID            string             `json:"id"`
	Domain        string             `json:"domain"`
	PhpVersion    string             `json:"php_version"`
	HTTPS         int                `json:"https"`
	Aliases       string             `json:"aliases"`
	PublicDir     string             `json:"public_dir"`
	ProjectConfig *projectConfigResp `json:"project_config"`
}

func (s configSite) aliasList() []string {
	var out []string
	_ = json.Unmarshal([]byte(s.Aliases), &out)
	return out
}

// The watcher polls every 2 s.
const configApplyTimeout = 15 * time.Second

func setProjectConfig(t *testing.T, on bool) {
	t.Helper()
	v := "false"
	if on {
		v = "true"
	}
	if body, status := httpPut(t, "/api/settings", map[string]string{"project_config_enabled": v}); status != http.StatusOK {
		t.Fatalf("PUT project_config_enabled=%s: status %d: %s", v, status, body)
	}
	t.Cleanup(func() { httpPut(t, "/api/settings", map[string]string{"project_config_enabled": "false"}) })
}

func getConfigSite(t *testing.T, id string) configSite {
	t.Helper()
	return decodeJSON[configSite](t, httpGet(t, "/api/sites/"+id))
}

func pollConfigSite(t *testing.T, id, what string, ok func(configSite) bool) configSite {
	t.Helper()
	var last configSite
	pollUntil(t, what, configApplyTimeout, func() bool {
		last = getConfigSite(t, id)
		return ok(last)
	})
	return last
}

func TestProjectConfig_SettingOffIgnoresTheFile(t *testing.T) {
	setProjectConfig(t, false)
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "php: \"8.1\"\nhttps: false\n"})
	id := createNodeSite(t, "config-off-go.test", dir)
	before := getConfigSite(t, id)

	time.Sleep(6 * time.Second)
	got := getConfigSite(t, id)
	if got.PhpVersion != before.PhpVersion || got.HTTPS != 1 {
		t.Errorf("php = %q, https = %d; want %q and 1 while the setting is off", got.PhpVersion, got.HTTPS, before.PhpVersion)
	}
	if got.ProjectConfig != nil {
		t.Errorf("project_config = %+v, want null while the setting is off", got.ProjectConfig)
	}
}

func TestProjectConfig_AppliedOnCreateAndOnChange(t *testing.T) {
	setProjectConfig(t, true)
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "php: \"8.1\"\nhttps: false\naliases: [alias-one.test]\n"})
	id := createNodeSite(t, "config-change-go.test", dir)

	got := pollConfigSite(t, id, "config applied after create", func(s configSite) bool {
		return s.PhpVersion == "8.1" && s.HTTPS == 0 && slices.Equal(s.aliasList(), []string{"alias-one.test"})
	})
	if got.ProjectConfig == nil {
		t.Fatal("project_config is null")
	}
	for _, f := range []string{"php_version", "https", "aliases"} {
		if !slices.Contains(got.ProjectConfig.Managed, f) {
			t.Errorf("managed = %v, want %s", got.ProjectConfig.Managed, f)
		}
	}
	if slices.Contains(got.ProjectConfig.Managed, "public_dir") {
		t.Errorf("managed = %v lists public_dir, which the file does not set", got.ProjectConfig.Managed)
	}
	if !slices.Equal(got.ProjectConfig.Files, []string{".devctl.yaml"}) {
		t.Errorf("files = %v", got.ProjectConfig.Files)
	}

	writeNodeFile(t, filepath.Join(dir, ".devctl.yaml"), "php: \"8.2\"\nhttps: true\naliases: [alias-two.test, www.alias-two.test]\n")
	pollConfigSite(t, id, "config applied after the file changed", func(s configSite) bool {
		return s.PhpVersion == "8.2" && s.HTTPS == 1 && slices.Equal(s.aliasList(), []string{"alias-two.test", "www.alias-two.test"})
	})
	if !caddyRouteExists(t, "vhost-"+id) {
		t.Error("Caddy vhost missing after the config change")
	}
}

func TestProjectConfig_AppliedOnDiscovery(t *testing.T) {
	setProjectConfig(t, true)
	serverRoot := os.Getenv("DEVCTL_SERVER_ROOT")
	if serverRoot == "" {
		serverRoot = "/home/testuser/ddev/sites/server"
	}
	watchDir := filepath.Dir(serverRoot)
	// Stage next to the watch dir (same file system) so the site appears in
	// one rename with its config file already in it.
	staging, err := os.MkdirTemp(filepath.Dir(watchDir), "devctl-test-config-staging-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(staging) })
	if err := os.Chmod(staging, 0777); err != nil {
		t.Fatal(err)
	}
	writeNodeFile(t, filepath.Join(staging, ".devctl.yaml"), "php: \"8.1\"\nhttps: false\naliases: [found.test]\n")
	siteDir := filepath.Join(watchDir, "devctl-test-config-discover")
	os.RemoveAll(siteDir)
	if err := os.Rename(staging, siteDir); err != nil {
		t.Fatalf("move site into the watch dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(siteDir) })

	site := pollSiteByDomain(t, "devctl-test-config-discover.test", 10*time.Second)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })
	pollConfigSite(t, site.ID, "config applied on discovery", func(s configSite) bool {
		return s.PhpVersion == "8.1" && s.HTTPS == 0 && slices.Equal(s.aliasList(), []string{"found.test"})
	})
}

func TestProjectConfig_TurningTheSettingOnAppliesExistingFiles(t *testing.T) {
	setProjectConfig(t, false)
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "public_dir: web\n"})
	id := createNodeSite(t, "config-enable-go.test", dir)
	setProjectConfig(t, true)
	pollConfigSite(t, id, "config applied when the setting turned on", func(s configSite) bool { return s.PublicDir == "web" })
}

func TestProjectConfig_LocalFileOverridesTheSharedFile(t *testing.T) {
	setProjectConfig(t, true)
	dir := nodeSiteDir(t, map[string]string{
		".devctl.yaml":       "php: \"8.1\"\nhttps: false\n",
		".devctl.local.yaml": "php: \"8.3\"\n",
	})
	id := createNodeSite(t, "config-local-go.test", dir)
	got := pollConfigSite(t, id, "local php and shared https applied", func(s configSite) bool {
		return s.PhpVersion == "8.3" && s.HTTPS == 0
	})
	if !slices.Equal(got.ProjectConfig.Files, []string{".devctl.yaml", ".devctl.local.yaml"}) {
		t.Errorf("files = %v", got.ProjectConfig.Files)
	}
}

func TestProjectConfig_InvalidFileReportsTheLineAndAppliesNothing(t *testing.T) {
	setProjectConfig(t, true)
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "https: false\nphp: \"8\"\n"})
	id := createNodeSite(t, "config-invalid-go.test", dir)
	got := pollConfigSite(t, id, "config error reported", func(s configSite) bool {
		return s.ProjectConfig != nil && s.ProjectConfig.Error != ""
	})
	if want := `.devctl.yaml: line 2: php: "8"`; !strings.Contains(got.ProjectConfig.Error, want) {
		t.Errorf("error = %q, want it to contain %q", got.ProjectConfig.Error, want)
	}
	time.Sleep(4 * time.Second)
	if got := getConfigSite(t, id); got.HTTPS != 1 || len(got.ProjectConfig.Managed) != 0 {
		t.Errorf("https = %d, managed = %v; want nothing applied from an invalid file", got.HTTPS, got.ProjectConfig.Managed)
	}
}

func TestProjectConfig_ReportsRequiredServices(t *testing.T) {
	setProjectConfig(t, true)
	installed := map[string]bool{}
	for _, svc := range decodeJSON[[]struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
	}](t, httpGet(t, "/api/services")) {
		installed[svc.ID] = svc.Installed
	}
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "services: [clickhouse, mailpit]\n"})
	id := createNodeSite(t, "config-services-go.test", dir)

	got := getConfigSite(t, id)
	if got.ProjectConfig == nil || len(got.ProjectConfig.Services) != 2 {
		t.Fatalf("project_config = %+v, want two services", got.ProjectConfig)
	}
	for _, svc := range got.ProjectConfig.Services {
		if svc.Label == "" {
			t.Errorf("%s has no label", svc.ID)
		}
		if missing := svc.Status == "missing"; missing == installed[svc.ID] {
			t.Errorf("%s status = %q, installed = %v", svc.ID, svc.Status, installed[svc.ID])
		}
	}
}

func TestProjectConfig_WorkersAreAddedAndRemoved(t *testing.T) {
	setProjectConfig(t, true)
	const cmd = "sleep 600"
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "workers:\n  - type: custom\n    command: " + cmd + "\n"})
	id := createNodeSite(t, "config-workers-go.test", dir)

	siteWorkers := func() []workerResp {
		return decodeJSON[[]workerResp](t, httpGet(t, "/api/workers?site_id="+id))
	}
	pollUntil(t, "custom worker added from the config", configApplyTimeout, func() bool {
		ws := siteWorkers()
		return len(ws) == 1 && ws[0].Type == "custom" && ws[0].Command == cmd && ws[0].Enabled
	})

	writeNodeFile(t, filepath.Join(dir, ".devctl.yaml"), "workers: []\n")
	pollUntil(t, "worker removed after the config dropped it", configApplyTimeout, func() bool { return len(siteWorkers()) == 0 })
}

func TestProjectConfig_NodeVersionIsWrittenToNvmrc(t *testing.T) {
	setProjectConfig(t, true)
	dir := nodeSiteDir(t, map[string]string{".devctl.yaml": "node: \"22\"\n"})
	id := createNodeSite(t, "config-node-go.test", dir)
	pollUntil(t, ".nvmrc written from the config", configApplyTimeout, func() bool {
		b, err := os.ReadFile(filepath.Join(dir, ".nvmrc"))
		return err == nil && string(b) == "22\n"
	})
	if got := getSiteNode(t, id); got.Version != "22" {
		t.Errorf("site node = %+v, want 22", got)
	}
}

func TestProjectConfig_InitWritesTheCurrentSettings(t *testing.T) {
	setProjectConfig(t, false)
	dir := nodeSiteDir(t, nil)
	id := createNodeSite(t, "config-init-go.test", dir)
	if _, status := httpPut(t, "/api/sites/"+id, map[string]any{
		"domain": "config-init-go.test", "root_path": dir, "php_version": "8.2", "public_dir": "public",
		"aliases": []string{"www.config-init-go.test"}, "https": 0, "cors": 1,
	}); status != http.StatusOK {
		t.Fatalf("PUT site: status %d", status)
	}

	body, status := httpPost(t, "/api/sites/"+id+"/config/init", nil)
	if status != http.StatusCreated {
		t.Fatalf("init: status %d: %s", status, body)
	}
	res := decodeJSON[struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}](t, body)
	if res.Path != filepath.Join(dir, ".devctl.yaml") {
		t.Errorf("path = %q", res.Path)
	}
	onDisk, err := os.ReadFile(res.Path)
	if err != nil || string(onDisk) != res.Content {
		t.Fatalf("file on disk = %q (%v), want the returned content", onDisk, err)
	}
	for _, want := range []string{
		"# yaml-language-server: $schema=https://raw.githubusercontent.com/godismyjudge95/devctl/main/projectconfig/schema.json\n",
		"php: \"8.2\"\n", "public_dir: public\n", "https: false\n", "cors: true\n", "spx: false\n", "- www.config-init-go.test\n",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("content lacks %q:\n%s", want, res.Content)
		}
	}

	if body, status := httpPost(t, "/api/sites/"+id+"/config/init", nil); status != http.StatusConflict {
		t.Errorf("second init: status %d, want 409: %s", status, body)
	}
	if body, status := httpPost(t, "/api/sites/"+id+"/config/init?force=true", nil); status != http.StatusCreated {
		t.Errorf("forced init: status %d, want 201: %s", status, body)
	}

	// Applying the file it wrote changes nothing.
	setProjectConfig(t, true)
	time.Sleep(5 * time.Second)
	if got := getConfigSite(t, id); got.PhpVersion != "8.2" || got.HTTPS != 0 || got.PublicDir != "public" {
		t.Errorf("site after applying its own config = %+v", got)
	}
}

func TestProjectConfig_InitCLI(t *testing.T) {
	dir := nodeSiteDir(t, nil)
	createNodeSite(t, "config-init-cli-go.test", dir)
	out := devctlCLIOK(t, dir, nil, "sites:config:init", ".", "--json")
	res := decodeJSON[struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}](t, []byte(out))
	if res.Path != filepath.Join(dir, ".devctl.yaml") || !strings.Contains(res.Content, "$schema=") {
		t.Errorf("sites:config:init --json = %+v", res)
	}
	if _, _, err := devctlCLI(t, dir, nil, "sites:config:init", "."); err == nil {
		t.Error("second sites:config:init succeeded, want an error because the file exists")
	}
	devctlCLIOK(t, dir, nil, "sites:config:init", ".", "--force")
}

func TestProjectConfig_SchemaIsServed(t *testing.T) {
	body := httpGet(t, "/api/project-config/schema")
	var schema struct {
		ID         string                     `json:"$id"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(schema.ID, "/projectconfig/schema.json") || schema.Properties["php"] == nil {
		t.Errorf("schema = %s", body)
	}
}

func TestProjectConfig_SettingMustBeABool(t *testing.T) {
	if body, status := httpPut(t, "/api/settings", map[string]string{"project_config_enabled": "maybe"}); status != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", status, body)
	}
	resolved := decodeJSON[map[string]string](t, httpGet(t, "/api/settings/resolved"))
	if v := resolved["project_config_enabled"]; v != "false" {
		t.Errorf("default project_config_enabled = %q, want false", v)
	}
}
