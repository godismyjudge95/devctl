package mcpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// client is a thin HTTP client that talks to the devctl API.
type client struct {
	base string
	http *http.Client
}

func newClient(addr string) *client {
	return &client{
		base: "http://" + addr,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *client) get(path string, out any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) getRaw(path string) (string, error) {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func (c *client) post(path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = strings.NewReader(string(b))
	}
	resp, err := c.http.Post(c.base+path, "application/json", r)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *client) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *client) put(path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.base+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ---- API types ----

type site struct {
	ID           string `json:"id"`
	Domain       string `json:"domain"`
	RootPath     string `json:"root_path"`
	PHPVersion   string `json:"php_version"`
	Aliases      string `json:"aliases"`
	SPXEnabled   int64  `json:"spx_enabled"`
	HTTPS        int64  `json:"https"`
	PublicDir    string `json:"public_dir"`
	Framework    string `json:"framework"`
	IsGitRepo    int64  `json:"is_git_repo"`
	GitRemoteURL string `json:"git_remote_url"`
}

type serviceState struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Status          string `json:"status"`
	Version         string `json:"version"`
	Installed       bool   `json:"installed"`
	Installable     bool   `json:"installable"`
	Required        bool   `json:"required"`
	Description     string `json:"description"`
	HasCredentials  bool   `json:"has_credentials"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

type phpVersion struct {
	Version   string `json:"version"`
	FPMSocket string `json:"fpm_socket"`
	Status    string `json:"status"`
}

type phpSettings struct {
	UploadMaxFilesize string `json:"upload_max_filesize"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  string `json:"max_execution_time"`
	PostMaxSize       string `json:"post_max_size"`
}

type spxProfile struct {
	Key             string  `json:"key"`
	PHPVersion      string  `json:"php_version"`
	Domain          string  `json:"domain"`
	Method          string  `json:"method"`
	URI             string  `json:"uri"`
	WallTimeMs      float64 `json:"wall_time_ms"`
	PeakMemoryBytes int64   `json:"peak_memory_bytes"`
	CalledFuncCount int64   `json:"called_func_count"`
	Timestamp       int64   `json:"timestamp"`
}

type dump struct {
	ID         int64   `json:"id"`
	File       *string `json:"file"`
	Line       *int64  `json:"line"`
	Nodes      string  `json:"nodes"`
	Timestamp  float64 `json:"timestamp"`
	SiteDomain *string `json:"site_domain"`
}

type dumpsResponse struct {
	Dumps []dump `json:"dumps"`
	Total int64  `json:"total"`
}

type logFileInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type spxFunction struct {
	Name         string  `json:"name"`
	Calls        int     `json:"calls"`
	InclusiveMs  float64 `json:"inclusive_ms"`
	ExclusiveMs  float64 `json:"exclusive_ms"`
	InclusivePct float64 `json:"inclusive_pct"`
	ExclusivePct float64 `json:"exclusive_pct"`
}

type spxProfileDetail struct {
	Key             string        `json:"key"`
	PHPVersion      string        `json:"php_version"`
	Domain          string        `json:"domain"`
	Method          string        `json:"method"`
	URI             string        `json:"uri"`
	WallTimeMs      float64       `json:"wall_time_ms"`
	PeakMemoryBytes int64         `json:"peak_memory_bytes"`
	CalledFuncCount int64         `json:"called_func_count"`
	Timestamp       int64         `json:"timestamp"`
	Functions       []spxFunction `json:"functions"`
}

// ---- Convenience wrappers ----

func (c *client) listSites() ([]site, error) {
	var out []site
	return out, c.get("/api/sites", &out)
}

func (c *client) updateSite(id string, body map[string]any) (site, error) {
	var out site
	return out, c.put("/api/sites/"+id, body, &out)
}

func (c *client) listServices() ([]serviceState, error) {
	var out []serviceState
	return out, c.get("/api/services", &out)
}

func (c *client) restartService(id string) error {
	return c.post("/api/services/"+id+"/restart", nil, nil)
}

func (c *client) listPHPVersions() ([]phpVersion, error) {
	var out []phpVersion
	return out, c.get("/api/php/versions", &out)
}

func (c *client) getPHPSettings() (phpSettings, error) {
	var out phpSettings
	return out, c.get("/api/php/settings", &out)
}

func (c *client) enableSPX(siteID string) error {
	return c.post("/api/sites/"+siteID+"/spx/enable", nil, nil)
}

func (c *client) disableSPX(siteID string) error {
	return c.post("/api/sites/"+siteID+"/spx/disable", nil, nil)
}

func (c *client) listSPXProfiles(domain string) ([]spxProfile, error) {
	var out []spxProfile
	path := "/api/spx/profiles"
	if domain != "" {
		path += "?domain=" + domain
	}
	return out, c.get(path, &out)
}

func (c *client) listDumps(siteDomain string) (dumpsResponse, error) {
	var out dumpsResponse
	path := "/api/dumps?limit=20"
	if siteDomain != "" {
		path += "&site=" + siteDomain
	}
	return out, c.get(path, &out)
}

func (c *client) listLogFiles() ([]logFileInfo, error) {
	var out []logFileInfo
	return out, c.get("/api/logs", &out)
}

func (c *client) getLogTail(id string, bytes int) (string, error) {
	path := fmt.Sprintf("/api/logs/%s/tail", id)
	if bytes > 0 {
		path += fmt.Sprintf("?bytes=%d", bytes)
	}
	return c.getRaw(path)
}

func (c *client) clearLog(id string) error {
	return c.delete("/api/logs/" + id)
}

func (c *client) startService(id string) error {
	return c.post("/api/services/"+id+"/start", nil, nil)
}

func (c *client) stopService(id string) error {
	return c.post("/api/services/"+id+"/stop", nil, nil)
}

func (c *client) getServiceCredentials(id string) (map[string]string, error) {
	var out map[string]string
	return out, c.get("/api/services/"+id+"/credentials", &out)
}

func (c *client) setPHPSettings(s phpSettings) (phpSettings, error) {
	var out phpSettings
	return out, c.put("/api/php/settings", s, &out)
}

func (c *client) getSettings() (map[string]string, error) {
	var out map[string]string
	return out, c.get("/api/settings/resolved", &out)
}

func (c *client) setSettings(s map[string]string) error {
	return c.put("/api/settings", s, nil)
}

func (c *client) checkDNSSetup() (bool, error) {
	var out struct {
		Configured bool `json:"configured"`
	}
	return out.Configured, c.get("/api/dns/setup", &out)
}

func (c *client) configureDNS() error {
	return c.post("/api/dns/setup", nil, nil)
}

func (c *client) teardownDNS() error {
	return c.delete("/api/dns/setup")
}

func (c *client) trustCA() (string, error) {
	var out struct {
		Status string `json:"status"`
		Output string `json:"output"`
	}
	return out.Output, c.post("/api/tls/trust", nil, &out)
}

func (c *client) getSPXProfileDetail(key string) (spxProfileDetail, error) {
	var out spxProfileDetail
	return out, c.get("/api/spx/profiles/"+key, &out)
}

func (c *client) clearDumps() error {
	return c.delete("/api/dumps")
}
