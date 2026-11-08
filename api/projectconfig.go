package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/node"
	"github.com/danielgormly/devctl/projectconfig"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/workers"
)

// projectConfigSetting turns the project config file on. While it is off,
// devctl does not read the file.
const projectConfigSetting = "project_config_enabled"

func (s *Server) projectConfigEnabled(ctx context.Context) bool {
	v, err := s.queries.GetSetting(ctx, projectConfigSetting)
	if err != nil {
		return false
	}
	on, _ := strconv.ParseBool(v)
	return on
}

// projectConfigView is what a site's config file controls, for the UI.
type projectConfigView struct {
	Files []string `json:"files"`
	// Managed lists the site fields the file sets, by their API names.
	Managed  []string          `json:"managed"`
	Error    string            `json:"error,omitempty"`
	Services []requiredService `json:"services"`
}

// requiredService is a service the project lists. Status is "missing" when
// it is not installed, else the service status (running, stopped, …).
type requiredService struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
}

// projectConfigFor returns nil when the site has no config file.
func projectConfigFor(root string, states map[string]services.ServiceState) *projectConfigView {
	cfg, files, err := projectconfig.Load(root)
	if len(files) == 0 {
		return nil
	}
	v := &projectConfigView{Files: files, Managed: []string{}, Services: []requiredService{}}
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.Managed = cfg.Managed()
	if cfg.Services != nil {
		for _, id := range *cfg.Services {
			st := states[id]
			rs := requiredService{ID: id, Label: st.Label, Status: string(st.Status)}
			if rs.Label == "" {
				rs.Label = id
			}
			if !st.Installed {
				rs.Status = "missing"
			}
			v.Services = append(v.Services, rs)
		}
	}
	return v
}

func (s *Server) serviceStatesByID() map[string]services.ServiceState {
	out := map[string]services.ServiceState{}
	for _, st := range s.poller.CurrentStates() {
		out[st.ID] = st
	}
	return out
}

// projectSite reads the values of a site that a config can set, and its
// workers.
func (s *Server) projectSite(ctx context.Context, site dbq.Site) (projectconfig.Site, []workers.State, error) {
	ws, err := s.workers.List(ctx, site.ID)
	if err != nil {
		return projectconfig.Site{}, nil, err
	}
	var aliases []string
	_ = json.Unmarshal([]byte(site.Aliases), &aliases)
	cur := projectconfig.Site{
		PHP:       site.PhpVersion,
		Node:      node.Find(site.RootPath).Version,
		PublicDir: site.PublicDir,
		HTTPS:     site.Https == 1,
		CORS:      site.Cors == 1,
		SPX:       site.SpxEnabled == 1,
		Aliases:   aliases,
	}
	for _, w := range ws {
		cur.Workers = append(cur.Workers, projectconfig.Worker{Type: w.Type, Command: w.Command})
	}
	return cur, ws, nil
}

// applyProjectConfig makes the site match its config file through the same
// code paths as the site settings form, the Node version, and workers.
func (s *Server) applyProjectConfig(ctx context.Context, site dbq.Site) error {
	cfg, files, err := projectconfig.Load(site.RootPath)
	if err != nil || len(files) == 0 {
		return err
	}
	cur, ws, err := s.projectSite(ctx, site)
	if err != nil {
		return err
	}
	plan := cfg.Plan(cur)
	if plan.Empty() {
		return nil
	}
	log.Printf("project config: %s: %s", site.Domain, strings.Join(plan.Changes, ", "))

	if plan.SiteRow {
		want := plan.Want
		aliases, _ := json.Marshal(want.Aliases)
		updated, err := s.saveSite(ctx, dbq.UpdateSiteParams{
			Domain:       site.Domain,
			RootPath:     site.RootPath,
			PhpVersion:   want.PHP,
			Aliases:      string(aliases),
			SpxEnabled:   boolInt(want.SPX),
			Https:        boolInt(want.HTTPS),
			Cors:         boolInt(want.CORS),
			Settings:     site.Settings,
			PublicDir:    want.PublicDir,
			IsGitRepo:    site.IsGitRepo,
			GitRemoteUrl: site.GitRemoteUrl,
			Framework:    site.Framework,
			ID:           site.ID,
		})
		if err != nil {
			return err
		}
		if plan.SPX {
			s.reloadPHPConfigs(updated.PhpVersion)
		}
	}

	var errs []error
	if plan.Node {
		errs = append(errs, node.WriteNvmrc(site.RootPath, plan.Want.Node))
	}
	for _, i := range plan.RemoveWorkers {
		errs = append(errs, s.workers.Remove(ctx, ws[i].ID))
	}
	for _, w := range plan.AddWorkers {
		_, err := s.workers.Add(ctx, site.ID, w.Type, w.Command)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// WatchProjectConfigs applies each site's config file when devctl finds the
// site, when the file changes, at startup, and when the setting turns on.
// It polls file stats: the sites watcher only sees the sites directory, and
// sites can live anywhere.
func (s *Server) WatchProjectConfigs(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	seen := map[string]string{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !s.projectConfigEnabled(ctx) {
			clear(seen)
			continue
		}
		all, err := s.queries.GetUserSites(ctx)
		if err != nil {
			continue
		}
		next := make(map[string]string, len(all))
		for _, site := range all {
			// created_at tells a site deleted and added again apart.
			key := site.ID + "|" + site.CreatedAt.String()
			fp := projectconfig.Fingerprint(site.RootPath)
			next[key] = fp
			if prev, ok := seen[key]; (ok && prev == fp) || fp == "" {
				continue
			}
			if err := s.applyProjectConfig(ctx, site); err != nil {
				log.Printf("project config: %s: %v", site.Domain, err)
			}
		}
		seen = next
	}
}

// GET /api/project-config/schema
func (s *Server) handleProjectConfigSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	_, _ = w.Write(projectconfig.Schema)
}

// POST /api/sites/{id}/config/init[?force=true] writes .devctl.yaml from the
// current site values.
func (s *Server) handleInitProjectConfig(w http.ResponseWriter, r *http.Request) {
	site, err := s.queries.GetSite(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, "site not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(site.RootPath, projectconfig.FileName)
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
	if _, err := os.Stat(path); err == nil && !force {
		writeError(w, path+" already exists. Use force to replace it", http.StatusConflict)
		return
	}
	cur, _, err := s.projectSite(r.Context(), site)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := projectconfig.Encode(cur)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path, "content": string(data)})
}
