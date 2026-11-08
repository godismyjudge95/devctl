package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/node"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
	"github.com/danielgormly/devctl/tools"
)

// siteNode is the Node version of one site: what its files ask for and the
// installed version the shims run for it.
type siteNode struct {
	node.Spec
	Resolved string `json:"resolved"`
}

// siteView is a site row plus values read from the site directory.
type siteView struct {
	dbq.Site
	Node siteNode `json:"node"`
	// ProjectConfig is null while project config is off or the site has no
	// config file.
	ProjectConfig *projectConfigView `json:"project_config"`
	// Proxy is null for a PHP site.
	Proxy *sites.Proxy `json:"proxy"`
}

func nodeFor(rootPath string, installed []node.Version) siteNode {
	if rootPath == "" {
		return siteNode{}
	}
	spec := node.Find(rootPath)
	out := siteNode{Spec: spec}
	if v, ok := node.Resolve(spec.Version, installed); ok {
		out.Resolved = v.String()
	}
	return out
}

func (s *Server) siteViews(all []dbq.Site) []siteView {
	installed := node.Installed(s.serverRoot)
	var states map[string]services.ServiceState
	if s.projectConfigEnabled(context.Background()) {
		states = s.serviceStatesByID()
	}
	out := make([]siteView, 0, len(all))
	for _, site := range all {
		v := siteView{Site: site, Node: nodeFor(site.RootPath, installed), Proxy: sites.ProxyFromSettings(site.Settings)}
		if states != nil {
			v.ProjectConfig = projectConfigFor(site.RootPath, states)
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) siteView(site dbq.Site) siteView {
	return s.siteViews([]dbq.Site{site})[0]
}

type nodeVersionRow struct {
	Version string   `json:"version"`
	Default bool     `json:"default"`
	Sites   []string `json:"sites"`
}

type nodeState struct {
	FnmInstalled bool             `json:"fnm_installed"`
	Versions     []nodeVersionRow `json:"versions"`
}

// GET /api/node
func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	installed := node.Installed(s.serverRoot)
	def, _ := node.Resolve("", installed)
	rows := make([]nodeVersionRow, 0, len(installed))
	index := map[string]int{}
	for i, v := range installed {
		rows = append(rows, nodeVersionRow{Version: v.String(), Default: v == def, Sites: []string{}})
		index[v.String()] = i
	}
	if all, err := s.queries.GetUserSites(r.Context()); err == nil {
		for _, site := range all {
			if i, ok := index[nodeFor(site.RootPath, installed).Resolved]; ok {
				rows[i].Sites = append(rows[i].Sites, site.Domain)
			}
		}
	}
	_, err := os.Stat(node.FnmPath(s.serverRoot))
	writeJSON(w, nodeState{FnmInstalled: err == nil, Versions: rows})
}

// POST /api/node/install?version=22 streams fnm install output as SSE. It
// installs the fnm helper first when it is missing.
func (s *Server) handleNodeInstall(w http.ResponseWriter, r *http.Request) {
	args, err := node.InstallArgs(strings.TrimSpace(r.URL.Query().Get("version")))
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	pw := &sseLineWriter{w: w, flusher: flusher, event: "output"}

	if _, err := os.Stat(node.FnmPath(s.serverRoot)); err != nil {
		if err := tools.EnsureLatest(r.Context(), tools.FNM, paths.BinDir(s.serverRoot), pw); err != nil {
			sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
			return
		}
		s.DeleteLatestVersion(tools.FNM.Name)
	}

	cmd := node.Fnm(r.Context(), s.serverRoot, args...)
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Run(); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": "fnm install: " + err.Error()})
		return
	}
	if err := node.EnsureShims(s.serverRoot); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": "node shims: " + err.Error()})
		return
	}
	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})
}

// DELETE /api/node/versions/{version} removes one exact installed version.
func (s *Server) handleNodeUninstall(w http.ResponseWriter, r *http.Request) {
	v, ok := node.ParseVersion(r.PathValue("version"))
	if !ok || !slices.Contains(node.Installed(s.serverRoot), v) {
		writeError(w, fmt.Sprintf("Node %s is not installed", r.PathValue("version")), http.StatusNotFound)
		return
	}
	if out, err := node.Fnm(r.Context(), s.serverRoot, "uninstall", v.String()).CombinedOutput(); err != nil {
		writeError(w, fmt.Sprintf("fnm uninstall: %v: %s", err, strings.TrimSpace(string(out))), http.StatusInternalServerError)
		return
	}
	if err := node.EnsureShims(s.serverRoot); err != nil {
		writeError(w, "node shims: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok", "version": v.String()})
}

// PUT /api/sites/{id}/node pins the site to a Node version by writing .nvmrc
// in its root directory.
func (s *Server) handleSetSiteNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	site, err := s.queries.GetSite(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	if site.RootPath == "" {
		writeError(w, site.Domain+" is a proxy site with no project directory", http.StatusBadRequest)
		return
	}
	if err := node.WriteNvmrc(site.RootPath, req.Version); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, node.ErrInvalidVersion) {
			code = http.StatusBadRequest
		}
		writeError(w, err.Error(), code)
		return
	}
	writeJSON(w, nodeFor(site.RootPath, node.Installed(s.serverRoot)))
}
