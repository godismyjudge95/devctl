//go:build integration

package apitest

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type siteNodeInfo struct {
	Version  string `json:"version"`
	File     string `json:"file"`
	Resolved string `json:"resolved"`
}

type siteWithNode struct {
	ID     string       `json:"id"`
	Domain string       `json:"domain"`
	Node   siteNodeInfo `json:"node"`
}

type nodeStateResp struct {
	FnmInstalled bool `json:"fnm_installed"`
	Versions     []struct {
		Version string   `json:"version"`
		Default bool     `json:"default"`
		Sites   []string `json:"sites"`
	} `json:"versions"`
}

// nodeSiteDir makes a site directory the daemon (site user) can write to and
// writes the given files into it.
func nodeSiteDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "devctl-node-site-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeNodeFile(t, filepath.Join(dir, name), content)
	}
	return dir
}

func writeNodeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
}

func createNodeSite(t *testing.T, domain, dir string) string {
	t.Helper()
	for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
		if s.Domain == domain {
			httpDelete(t, "/api/sites/"+s.ID)
		}
	}
	body, status := httpPost(t, "/api/sites", map[string]any{"domain": domain, "root_path": dir})
	if status != http.StatusCreated {
		t.Fatalf("create site: status %d: %s", status, body)
	}
	site := decodeJSON[siteWithNode](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })
	return site.ID
}

func getSiteNode(t *testing.T, id string) siteNodeInfo {
	t.Helper()
	return decodeJSON[siteWithNode](t, httpGet(t, "/api/sites/"+id)).Node
}

func TestNode_SiteReportsVersionFromFiles(t *testing.T) {
	dir := nodeSiteDir(t, map[string]string{"package.json": `{"name":"app","engines":{"node":">=18"}}`})
	id := createNodeSite(t, "node-files-go.test", dir)

	got := getSiteNode(t, id)
	if got.Version != ">=18" || got.File != filepath.Join(dir, "package.json") {
		t.Fatalf("site node = %+v, want >=18 from package.json", got)
	}

	writeNodeFile(t, filepath.Join(dir, ".nvmrc"), "lts/*\n")
	got = getSiteNode(t, id)
	if got.Version != "lts/*" || got.File != filepath.Join(dir, ".nvmrc") {
		t.Fatalf("site node after .nvmrc = %+v, want lts/* from .nvmrc (it wins over package.json)", got)
	}

	var listed *siteWithNode
	all := decodeJSON[[]siteWithNode](t, httpGet(t, "/api/sites"))
	for i := range all {
		if all[i].ID == id {
			listed = &all[i]
		}
	}
	if listed == nil || listed.Node.Version != "lts/*" {
		t.Fatalf("GET /api/sites node for %s = %+v, want lts/*", id, listed)
	}
}

func TestNode_SiteWithoutVersionFile(t *testing.T) {
	id := createNodeSite(t, "node-none-go.test", nodeSiteDir(t, nil))
	if got := getSiteNode(t, id); got.Version != "" || got.File != "" {
		t.Fatalf("site node = %+v, want no version", got)
	}
}

func TestNode_SetSiteVersionWritesNvmrc(t *testing.T) {
	dir := nodeSiteDir(t, map[string]string{"package.json": `{"engines":{"node":"18"}}`})
	id := createNodeSite(t, "node-set-go.test", dir)

	body, status := httpPut(t, "/api/sites/"+id+"/node", map[string]string{"version": "22"})
	if status != http.StatusOK {
		t.Fatalf("PUT node: status %d: %s", status, body)
	}
	if got := decodeJSON[siteNodeInfo](t, body); got.Version != "22" || got.File != filepath.Join(dir, ".nvmrc") {
		t.Fatalf("PUT node response = %+v, want 22 from .nvmrc", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".nvmrc"))
	if err != nil || string(b) != "22\n" {
		t.Fatalf(".nvmrc = %q, %v; want %q", b, err, "22\n")
	}
	if got := getSiteNode(t, id); got.Version != "22" {
		t.Fatalf("GET site after PUT = %+v, want 22", got)
	}

	for _, bad := range []string{"", "system", "20\n18"} {
		if _, status := httpPut(t, "/api/sites/"+id+"/node", map[string]string{"version": bad}); status != http.StatusBadRequest {
			t.Errorf("PUT node %q: status %d, want 400", bad, status)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".nvmrc")); string(b) != "22\n" {
		t.Fatalf(".nvmrc changed by rejected PUTs: %q", b)
	}

	if _, status := httpPut(t, "/api/sites/no-such-site/node", map[string]string{"version": "22"}); status != http.StatusNotFound {
		t.Errorf("PUT node on unknown site: status %d, want 404", status)
	}
}

func TestNode_ListReturnsVersionsArray(t *testing.T) {
	body := httpGet(t, "/api/node")
	if !strings.Contains(string(body), `"versions":[`) {
		t.Fatalf("GET /api/node = %s, want a versions array", body)
	}
}

func TestNode_InstallRejectsBadVersion(t *testing.T) {
	for _, q := range []string{"", "?version=", "?version=--help", "?version=%3E%3D20", "?version=20%3Brm"} {
		if body, status := httpPost(t, "/api/node/install"+q, nil); status != http.StatusBadRequest {
			t.Errorf("POST /api/node/install%s: status %d, want 400: %s", q, status, body)
		}
	}
}

func TestNode_UninstallUnknownVersion(t *testing.T) {
	for _, v := range []string{"v1.2.3", "not-a-version"} {
		if _, status := httpDelete(t, "/api/node/versions/"+v); status != http.StatusNotFound {
			t.Errorf("DELETE /api/node/versions/%s: status %d, want 404", v, status)
		}
	}
}

// TestNode_InstallShimsAndUninstall downloads one real Node release.
func TestNode_InstallShimsAndUninstall(t *testing.T) {
	res := httpSSE(t, http.MethodPost, "/api/node/install?version=22", 10*time.Minute)
	if res.LastEvent != "done" {
		t.Fatalf("install Node 22: last event %q: %s", res.LastEvent, res.LastData)
	}

	var installed string
	state := decodeJSON[nodeStateResp](t, httpGet(t, "/api/node"))
	for _, v := range state.Versions {
		if strings.HasPrefix(v.Version, "v22.") {
			installed = v.Version
		}
	}
	if !state.FnmInstalled || installed == "" {
		t.Fatalf("GET /api/node after install = %+v, want fnm and a v22 version", state)
	}
	t.Cleanup(func() { httpDelete(t, "/api/node/versions/"+installed) })

	dir := nodeSiteDir(t, map[string]string{".nvmrc": "22\n"})
	id := createNodeSite(t, "node-install-go.test", dir)
	if got := getSiteNode(t, id); got.Resolved != installed {
		t.Fatalf("site node = %+v, want resolved %s", got, installed)
	}

	shim := filepath.Join(doctorServerRoot(), "bin", "node")
	cmd := exec.Command(shim, "--version")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != installed {
		t.Fatalf("node shim in site dir: %q, %v; want %s", out, err, installed)
	}
	npm := exec.Command(filepath.Join(doctorServerRoot(), "bin", "npm"), "--version")
	npm.Dir = dir
	if out, err := npm.CombinedOutput(); err != nil {
		t.Fatalf("npm shim: %v\n%s", err, out)
	}

	body, status := httpDelete(t, "/api/node/versions/"+installed)
	if status != http.StatusOK {
		t.Fatalf("uninstall %s: status %d: %s", installed, status, body)
	}
	state = decodeJSON[nodeStateResp](t, httpGet(t, "/api/node"))
	for _, v := range state.Versions {
		if v.Version == installed {
			t.Fatalf("%s still listed after uninstall", installed)
		}
	}
	if len(state.Versions) == 0 {
		if _, err := os.Stat(shim); !os.IsNotExist(err) {
			t.Fatalf("node shim still present with no Node installed (err %v)", err)
		}
	}
}
