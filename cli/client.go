package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgormly/devctl/newsite"
)

// Client is a thin HTTP client that talks to the devctl daemon API.
type Client struct {
	base string
	http *http.Client
}

// NewClient creates a client targeting addr (e.g. "127.0.0.1:4000").
func NewClient(addr string) *Client {
	return &Client{
		base: "http://" + addr,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(path string, out any) error {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) getRaw(path string) (string, error) {
	resp, err := c.http.Get(c.base + path)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func (c *Client) post(path string, body any, out any) error {
	return c.postTimeout(path, body, out, c.http.Timeout)
}

func (c *Client) postTimeout(path string, body any, out any, timeout time.Duration) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = strings.NewReader(string(b))
	}
	client := &http.Client{Timeout: timeout, Transport: c.http.Transport}
	resp, err := client.Post(c.base+path, "application/json", r)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) put(path string, body any, out any) error {
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
		return fmt.Errorf("%s", strings.TrimSpace(string(rb)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) delete(path string) error {
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
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *Client) deleteWithBody(path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, c.base+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(rb)))
	}
	return nil
}

// ---- API types ----

type Site struct {
	ID             string  `json:"id"`
	Domain         string  `json:"domain"`
	RootPath       string  `json:"root_path"`
	PHPVersion     string  `json:"php_version"`
	Aliases        string  `json:"aliases"`
	SPXEnabled     int64   `json:"spx_enabled"`
	HTTPS          int64   `json:"https"`
	CORS           int64   `json:"cors"`
	PublicDir      string  `json:"public_dir"`
	Framework      string  `json:"framework"`
	IsGitRepo      int64   `json:"is_git_repo"`
	GitRemoteURL   string  `json:"git_remote_url"`
	ParentSiteID   *string `json:"parent_site_id"`
	WorktreeBranch *string `json:"worktree_branch"`
}

type WorktreeConfig struct {
	Symlinks []string `json:"symlinks"`
	Copies   []string `json:"copies"`
	NoShare  bool     `json:"no_share,omitempty"`
}

type Branch struct {
	Name      string `json:"name"`
	IsRemote  bool   `json:"is_remote"`
	IsCurrent bool   `json:"is_current"`
}

type ServiceState struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Status          string `json:"status"`
	Version         string `json:"version"`
	Installed       bool   `json:"installed"`
	Installable     bool   `json:"installable"`
	Required        bool   `json:"required"`
	Description     string `json:"description"`
	InstallVersion  string `json:"install_version"`
	HasCredentials  bool   `json:"has_credentials"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

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

type PHPVersion struct {
	Version   string       `json:"version"`
	FPMSocket string       `json:"fpm_socket"`
	Status    string       `json:"status"`
	Xdebug    XdebugStatus `json:"xdebug"`
}

type XdebugStatus struct {
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
}

type PHPSettings struct {
	UploadMaxFilesize string   `json:"upload_max_filesize"`
	MemoryLimit       string   `json:"memory_limit"`
	MaxExecutionTime  string   `json:"max_execution_time"`
	PostMaxSize       string   `json:"post_max_size"`
	Scope             string   `json:"scope"`
	Overrides         []string `json:"overrides"`
}

type PHPIniDirective struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SPXProfile struct {
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

type SPXFunction struct {
	Name         string  `json:"name"`
	Calls        int     `json:"calls"`
	InclusiveMs  float64 `json:"inclusive_ms"`
	ExclusiveMs  float64 `json:"exclusive_ms"`
	InclusivePct float64 `json:"inclusive_pct"`
	ExclusivePct float64 `json:"exclusive_pct"`
}

type SPXProfileDetail struct {
	Key             string        `json:"key"`
	PHPVersion      string        `json:"php_version"`
	Domain          string        `json:"domain"`
	Method          string        `json:"method"`
	URI             string        `json:"uri"`
	WallTimeMs      float64       `json:"wall_time_ms"`
	PeakMemoryBytes int64         `json:"peak_memory_bytes"`
	CalledFuncCount int64         `json:"called_func_count"`
	Timestamp       int64         `json:"timestamp"`
	Functions       []SPXFunction `json:"functions"`
}

type Dump struct {
	ID         int64   `json:"id"`
	File       *string `json:"file"`
	Line       *int64  `json:"line"`
	Nodes      string  `json:"nodes"`
	Timestamp  float64 `json:"timestamp"`
	SiteDomain *string `json:"site_domain"`
}

type DumpsResponse struct {
	Dumps []Dump `json:"dumps"`
	Total int64  `json:"total"`
}

type LogFileInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type MailAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

type MailMessage struct {
	ID      string        `json:"ID"`
	Read    bool          `json:"Read"`
	From    MailAddress   `json:"From"`
	To      []MailAddress `json:"To"`
	Cc      []MailAddress `json:"Cc"`
	Subject string        `json:"Subject"`
	Created string        `json:"Created"`
	Size    int64         `json:"Size"`
}

type MailMessageDetail struct {
	ID          string           `json:"ID"`
	Read        bool             `json:"Read"`
	From        MailAddress      `json:"From"`
	To          []MailAddress    `json:"To"`
	Cc          []MailAddress    `json:"Cc"`
	Subject     string           `json:"Subject"`
	Date        string           `json:"Date"`
	Size        int64            `json:"Size"`
	Text        string           `json:"Text"`
	HTML        string           `json:"HTML"`
	Attachments []MailAttachment `json:"Attachments"`
	Inline      []MailAttachment `json:"Inline"`
}

type MailAttachment struct {
	PartID      string `json:"PartID"`
	FileName    string `json:"FileName"`
	ContentType string `json:"ContentType"`
	Size        int64  `json:"Size"`
}

type MailListResponse struct {
	Total    int           `json:"total"`
	Unread   int           `json:"unread"`
	Count    int           `json:"count"`
	Start    int           `json:"start"`
	Messages []MailMessage `json:"messages"`
}

// ---- Convenience wrappers ----

func (c *Client) ListSites() ([]Site, error) {
	var out []Site
	return out, c.get("/api/sites", &out)
}

func (c *Client) ListSiteBranches(id string) ([]Branch, error) {
	var out []Branch
	return out, c.get("/api/sites/"+id+"/branches", &out)
}

func (c *Client) GetWorktreeConfig(id string) (WorktreeConfig, error) {
	var out WorktreeConfig
	return out, c.get("/api/sites/"+id+"/worktree-config", &out)
}

func (c *Client) PutWorktreeConfig(id string, cfg WorktreeConfig) (WorktreeConfig, error) {
	var out WorktreeConfig
	return out, c.put("/api/sites/"+id+"/worktree-config", cfg, &out)
}

func (c *Client) ListWorktrees(id string) ([]Site, error) {
	var out []Site
	return out, c.get("/api/sites/"+id+"/worktrees", &out)
}

func (c *Client) CreateWorktree(parentID string, body map[string]any) (Site, error) {
	var out Site
	return out, c.postTimeout("/api/sites/"+parentID+"/worktrees", body, &out, 10*time.Minute)
}

func (c *Client) RemoveWorktree(parentID, worktreeID string) error {
	return c.delete("/api/sites/" + parentID + "/worktrees/" + worktreeID)
}

func (c *Client) UpdateSite(id string, body map[string]any) (Site, error) {
	var out Site
	return out, c.put("/api/sites/"+id, body, &out)
}

func (c *Client) ListServices() ([]ServiceState, error) {
	var out []ServiceState
	return out, c.get("/api/services", &out)
}

func (c *Client) ListHelpers() ([]HelperState, error) {
	var out []HelperState
	return out, c.get("/api/helpers", &out)
}

func (c *Client) InstallHelperSSE(id string, onOutput func(line string)) error {
	return c.postSSE("/api/helpers/"+id+"/install", "install "+id, onOutput)
}

func (c *Client) UpdateHelperSSE(id string, onOutput func(line string)) error {
	return c.postSSE("/api/helpers/"+id+"/update", "update "+id, onOutput)
}

func (c *Client) UninstallHelper(id string) error {
	return c.delete("/api/helpers/" + id)
}

func (c *Client) RestartService(id string) error {
	return c.post("/api/services/"+id+"/restart", nil, nil)
}

func (c *Client) StartService(id string) error {
	return c.post("/api/services/"+id+"/start", nil, nil)
}

func (c *Client) StopService(id string) error {
	return c.post("/api/services/"+id+"/stop", nil, nil)
}

func (c *Client) GetServiceCredentials(id string) (map[string]string, error) {
	var out map[string]string
	return out, c.get("/api/services/"+id+"/credentials", &out)
}

// InstallServiceSSE POSTs to /api/services/{id}/install and streams the SSE
// response. For each "output" event, onOutput is called with the text payload.
// Returns nil on a "done" event, or an error if the stream ends with an "error"
// event or the connection drops unexpectedly.
//
// A dedicated long-timeout HTTP client is used because installs can take several
// minutes — the regular 15-second client would time out mid-stream.
func (c *Client) InstallServiceSSE(id string, onOutput func(line string)) error {
	return c.postSSE("/api/services/"+id+"/install", "install "+id, onOutput)
}

func (c *Client) postSSE(path, action string, onOutput func(line string)) error {
	_, err := c.streamSSE(path, action, nil, 10*time.Minute, onOutput)
	return err
}

// streamSSE POSTs body (JSON, when not nil) and reads the SSE response until a
// "done" event, whose data it returns, or an "error" event.
func (c *Client) streamSSE(path, action string, body any, timeout time.Duration, onOutput func(line string)) (json.RawMessage, error) {
	longClient := &http.Client{Timeout: timeout}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := longClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	var currentEvent, currentData string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent != "" {
			switch currentEvent {
			case "output":
				var text string
				if err := json.Unmarshal([]byte(currentData), &text); err != nil {
					text = currentData
				}
				if onOutput != nil {
					onOutput(text)
				}
			case "done":
				return json.RawMessage(currentData), nil
			case "error":
				var payload struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(currentData), &payload); err != nil {
					return nil, fmt.Errorf("%s failed: %s", action, currentData)
				}
				return nil, fmt.Errorf("%s failed: %s", action, payload.Error)
			}
			currentEvent = ""
			currentData = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SSE stream: %w", err)
	}
	return nil, fmt.Errorf("%s: stream ended without a done or error event", action)
}

// NewSiteResult is the "done" payload of POST /api/sites/new.
type NewSiteResult struct {
	Site Site   `json:"site"`
	URL  string `json:"url"`
}

// NewSiteSSE creates a project from a template, streaming installer output to
// onOutput. Laravel starter kits run npm, so the timeout is long.
func (c *Client) NewSiteSSE(req newsite.Request, onOutput func(line string)) (NewSiteResult, error) {
	var out NewSiteResult
	data, err := c.streamSSE("/api/sites/new", "create "+req.Name, req, 30*time.Minute, onOutput)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}

// UpdateServiceSSE POSTs to /api/services/{id}/update and streams the SSE
// response. For each "output" event, onOutput is called with the text payload.
// Returns nil on a "done" event, or an error on an "error" event or unexpected
// stream close. A long timeout is used because updates can take several minutes.
func (c *Client) UpdateServiceSSE(id string, onOutput func(line string)) error {
	return c.postSSE("/api/services/"+id+"/update", "update "+id, onOutput)
}

func (c *Client) ListPHPVersions() ([]PHPVersion, error) {
	var out []PHPVersion
	return out, c.get("/api/php/versions", &out)
}

func phpSettingsPath(version string) string {
	if version == "" {
		return "/api/php/settings"
	}
	return "/api/php/settings?version=" + url.QueryEscape(version)
}

// GetPHPSettings reads the settings for one PHP version, or for all versions
// when version is "".
func (c *Client) GetPHPSettings(version string) (PHPSettings, error) {
	var out PHPSettings
	return out, c.get(phpSettingsPath(version), &out)
}

// SetPHPSettings stores only the keys in kv, so keys the user did not name
// keep following their current source.
func (c *Client) SetPHPSettings(version string, kv map[string]string) (PHPSettings, error) {
	var out PHPSettings
	return out, c.put(phpSettingsPath(version), kv, &out)
}

func (c *Client) ListPHPIni(scope string) ([]PHPIniDirective, error) {
	var out []PHPIniDirective
	path := "/api/php/ini"
	if scope != "" {
		path += "?scope=" + url.QueryEscape(scope)
	}
	return out, c.get(path, &out)
}

func (c *Client) SetPHPIni(scope, key, value string) (PHPIniDirective, error) {
	var out PHPIniDirective
	return out, c.put("/api/php/ini/"+url.PathEscape(scope)+"/"+url.PathEscape(key), map[string]string{"value": value}, &out)
}

func (c *Client) UnsetPHPIni(scope, key string) error {
	return c.delete("/api/php/ini/" + url.PathEscape(scope) + "/" + url.PathEscape(key))
}

// SetPHPXdebug turns Xdebug on or off for one PHP version. mode "" means
// the daemon default.
func (c *Client) SetPHPXdebug(version string, enabled bool, mode string) (XdebugStatus, error) {
	var out XdebugStatus
	body := map[string]any{"enabled": enabled, "mode": mode}
	return out, c.put("/api/php/versions/"+url.PathEscape(version)+"/xdebug", body, &out)
}

func (c *Client) EnableSPX(siteID string) error {
	return c.post("/api/sites/"+siteID+"/spx/enable", nil, nil)
}

func (c *Client) DisableSPX(siteID string) error {
	return c.post("/api/sites/"+siteID+"/spx/disable", nil, nil)
}

func (c *Client) ListSPXProfiles(domain string) ([]SPXProfile, error) {
	var out []SPXProfile
	path := "/api/spx/profiles"
	if domain != "" {
		path += "?domain=" + domain
	}
	return out, c.get(path, &out)
}

func (c *Client) GetSPXProfileDetail(key string) (SPXProfileDetail, error) {
	var out SPXProfileDetail
	return out, c.get("/api/spx/profiles/"+key, &out)
}

func (c *Client) ListDumps(siteDomain string) ([]Dump, error) {
	var out []Dump
	path := "/api/dumps?limit=20"
	if siteDomain != "" {
		path += "&site=" + siteDomain
	}
	return out, c.get(path, &out)
}

func (c *Client) ClearDumps() error {
	return c.delete("/api/dumps")
}

func (c *Client) ListLogFiles() ([]LogFileInfo, error) {
	var out []LogFileInfo
	return out, c.get("/api/logs", &out)
}

func (c *Client) GetLogTail(id string, bytes int) (string, error) {
	path := fmt.Sprintf("/api/logs/%s/tail", id)
	if bytes > 0 {
		path += fmt.Sprintf("?bytes=%d", bytes)
	}
	return c.getRaw(path)
}

// StreamLog connects to the SSE log stream for id and writes decoded log lines
// to w until ctx is cancelled or the server closes the connection.
func (c *Client) StreamLog(ctx context.Context, id string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+fmt.Sprintf("/api/logs/%s", id), nil)
	if err != nil {
		return err
	}
	// Use a client without a timeout for the streaming connection.
	hc := &http.Client{}
	resp, err := hc.Do(req)
	if err != nil {
		// Context cancellation is a normal exit (user pressed Ctrl-C), not an error.
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}

	// Use bufio.Reader instead of bufio.Scanner. Scanner treats (n, io.EOF)
	// returned by the HTTP transport in a single Read call as terminal — it
	// stops even though the connection is still open and more data will arrive.
	// ReadString blocks until the next newline or an error; on io.EOF it means
	// "no full line yet", so we retry instead of stopping.
	reader := bufio.NewReader(resp.Body)
	for {
		// Check for cancellation before each read.
		if ctx.Err() != nil {
			return nil
		}
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			if line == "" {
				// Empty read on EOF means the server closed the connection cleanly.
				return nil
			}
			// Partial read (no trailing newline yet) — server hasn't sent more
			// data yet, or sent a partial line. Process what we have and loop.
			if strings.HasPrefix(line, "data:") {
				// Shouldn't normally happen (data lines are always newline-terminated),
				// but handle it gracefully.
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var chunk string
				if json.Unmarshal([]byte(data), &chunk) == nil {
					fmt.Fprint(w, chunk)
				}
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		// The server JSON-encodes the log chunk as a string.
		var chunk string
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Fallback: write raw data.
			fmt.Fprintln(w, data)
			continue
		}
		fmt.Fprint(w, chunk)
	}
}

func (c *Client) ClearLog(id string) error {
	return c.delete("/api/logs/" + id)
}

func (c *Client) GetSettings() (map[string]string, error) {
	var out map[string]string
	return out, c.get("/api/settings/resolved", &out)
}

func (c *Client) SetSettings(s map[string]string) error {
	return c.put("/api/settings", s, nil)
}

func (c *Client) CheckDNSSetup() (bool, error) {
	var out struct {
		Configured bool `json:"configured"`
	}
	return out.Configured, c.get("/api/dns/setup", &out)
}

func (c *Client) ConfigureDNS() error {
	return c.post("/api/dns/setup", nil, nil)
}

func (c *Client) TeardownDNS() error {
	return c.delete("/api/dns/setup")
}

type SelfUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
}

func (c *Client) GetSelfUpdateStatus() (SelfUpdateStatus, error) {
	var out SelfUpdateStatus
	return out, c.get("/api/self/update/status", &out)
}

// ApplySelfUpdateSSE POSTs to /api/self/update/apply and streams the SSE
// response. For each "output" event, onOutput is called with the text payload.
// Returns nil on a "done" event, or an error on an "error" event or unexpected
// stream close. A long timeout is used because the download can take a while.
func (c *Client) ApplySelfUpdateSSE(onOutput func(line string)) error {
	longClient := &http.Client{Timeout: 10 * time.Minute}

	req, err := http.NewRequest(http.MethodPost, c.base+"/api/self/update/apply", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := longClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /api/self/update/apply: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	var currentEvent, currentData string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent != "" {
			switch currentEvent {
			case "output":
				var text string
				if err := json.Unmarshal([]byte(currentData), &text); err != nil {
					text = currentData
				}
				if onOutput != nil {
					onOutput(text)
				}
			case "done":
				return nil
			case "error":
				var payload struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(currentData), &payload); err != nil {
					return fmt.Errorf("self-update failed: %s", currentData)
				}
				return fmt.Errorf("self-update failed: %s", payload.Error)
			}
			currentEvent = ""
			currentData = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading SSE stream: %w", err)
	}
	return fmt.Errorf("self-update: stream ended without a done or error event")
}

func (c *Client) TrustCA() (string, error) {
	var out struct {
		Status string `json:"status"`
		Output string `json:"output"`
	}
	return out.Output, c.post("/api/tls/trust", nil, &out)
}

// SearchMail lists messages newest first. An empty query lists all messages;
// otherwise it uses Mailpit's search syntax (to:, from:, subject:).
func (c *Client) SearchMail(query string, limit, start int) (MailListResponse, error) {
	var out MailListResponse
	path := fmt.Sprintf("/api/mail/api/v1/messages?limit=%d&start=%d", limit, start)
	if query != "" {
		path = fmt.Sprintf("/api/mail/api/v1/search?query=%s&limit=%d&start=%d", url.QueryEscape(query), limit, start)
	}
	return out, c.get(path, &out)
}

func (c *Client) GetMail(id string) (MailMessageDetail, error) {
	var out MailMessageDetail
	return out, c.get("/api/mail/api/v1/message/"+url.PathEscape(id), &out)
}

// GetMailRaw returns the full RFC 822 source of a message.
func (c *Client) GetMailRaw(id string) (string, error) {
	return c.getRaw("/api/mail/api/v1/message/" + url.PathEscape(id) + "/raw")
}

// GetMailPart returns the decoded bytes of one MIME part (an attachment).
func (c *Client) GetMailPart(id, partID string) (string, error) {
	return c.getRaw("/api/mail/api/v1/message/" + url.PathEscape(id) + "/part/" + url.PathEscape(partID))
}

func (c *Client) DeleteMail(ids []string) error {
	return c.deleteWithBody("/api/mail/api/v1/messages", map[string]any{"IDs": ids})
}

func (c *Client) DeleteAllMail() error {
	return c.delete("/api/mail/api/v1/messages")
}

// PostgresExtensionStatus mirrors install.ExtensionStatus for CLI display.
type PostgresExtensionStatus struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	FilesInstalled    bool   `json:"files_installed"`
	PreloadConfigured bool   `json:"preload_configured"`
	Wired             bool   `json:"wired"`
	Ready             bool   `json:"ready"`
	Version           string `json:"version,omitempty"`
	Note              string `json:"note,omitempty"`
	RequiresPeer      string `json:"requires_peer,omitempty"`
}

// ListPostgresExtensions calls GET /api/postgres/extensions.
func (c *Client) ListPostgresExtensions() ([]PostgresExtensionStatus, error) {
	var out []PostgresExtensionStatus
	return out, c.get("/api/postgres/extensions", &out)
}

// EnsurePostgresExtensions calls POST /api/postgres/extensions/ensure.
// Uses a long timeout because compiling pg_clickhouse can take several minutes.
func (c *Client) EnsurePostgresExtensions() (map[string]any, error) {
	var out map[string]any
	long := &http.Client{Timeout: 15 * time.Minute}
	resp, err := long.Post(c.base+"/api/postgres/extensions/ensure", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("POST /api/postgres/extensions/ensure: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

type DBEngine struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
}

type DBCatalog struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}

type DBColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DBQueryResult keeps cells as raw JSON so large integers and JSON columns
// print exactly as the engine returned them.
type DBQueryResult struct {
	Columns      []DBColumn          `json:"columns"`
	Rows         [][]json.RawMessage `json:"rows"`
	RowsAffected int64               `json:"rows_affected"`
	Limited      bool                `json:"limited"`
	DurationMS   int64               `json:"duration_ms"`
	Statement    string              `json:"statement"`
}

type DBImportResult struct {
	Status  string `json:"status"`
	Bytes   int64  `json:"bytes"`
	Created bool   `json:"created"`
}

func dbPath(engine, name string) string {
	return "/api/databases/" + url.PathEscape(engine) + "/databases/" + url.PathEscape(name)
}

func (c *Client) ListDBEngines() ([]DBEngine, error) {
	var out struct {
		Engines []DBEngine `json:"engines"`
	}
	err := c.get("/api/databases", &out)
	return out.Engines, err
}

func (c *Client) ListDatabases(engine string) ([]DBCatalog, error) {
	var out struct {
		Databases []DBCatalog `json:"databases"`
	}
	err := c.get("/api/databases/"+url.PathEscape(engine)+"/databases", &out)
	return out.Databases, err
}

func (c *Client) CreateDatabase(engine, name string) error {
	return c.post("/api/databases/"+url.PathEscape(engine)+"/databases", map[string]string{"name": name}, nil)
}

func (c *Client) DropDatabase(engine, name string) error {
	return c.delete(dbPath(engine, name))
}

func (c *Client) DuplicateDatabase(engine, from, to string) error {
	return c.postTimeout("/api/databases/"+url.PathEscape(engine)+"/databases/duplicate",
		map[string]string{"from": from, "to": to}, nil, 10*time.Minute)
}

func (c *Client) QueryDatabase(engine, name, sql string, limit int) (DBQueryResult, error) {
	var out DBQueryResult
	err := c.postTimeout("/api/databases/"+url.PathEscape(engine)+"/query",
		map[string]any{"database": name, "sql": sql, "limit": limit}, &out, 2*time.Minute)
	return out, err
}

// streamClient has no timeout: an import or export runs as long as the dump
// takes, and c.http gives up after 15 s.
func (c *Client) streamClient() *http.Client {
	return &http.Client{Transport: c.http.Transport}
}

// ImportDatabase streams body (plain or gzip SQL) into the database. With
// create, the daemon creates a missing database first.
func (c *Client) ImportDatabase(engine, name string, body io.Reader, create bool) (DBImportResult, error) {
	var out DBImportResult
	path := dbPath(engine, name) + "/import"
	if create {
		path += "?create=1"
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, body)
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/sql")
	resp, err := c.streamClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return out, apiError(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

// ExportDatabase copies a full SQL dump to w. The daemon aborts the
// connection when the engine fails mid-dump, so any read error means the
// dump in w is incomplete.
func (c *Client) ExportDatabase(engine, name string, w io.Writer) (int64, error) {
	path := dbPath(engine, name) + "/export"
	resp, err := c.streamClient().Get(c.base + path)
	if err != nil {
		return 0, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, apiError(resp)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("export of %s/%s failed after %s: %w", engine, name, formatBytes(n), err)
	}
	return n, nil
}

// apiError returns the daemon's {"error": "..."} message, or the raw body.
func apiError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &body) == nil && body.Error != "" {
		return errors.New(body.Error)
	}
	if msg := strings.TrimSpace(string(b)); msg != "" {
		return errors.New(msg)
	}
	return errors.New(resp.Status)
}

// FormatAddress formats a MailAddress for display.
func FormatAddress(a MailAddress) string {
	if a.Name != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Address)
	}
	return a.Address
}

// FormatAddresses formats a slice of MailAddress for display.
func FormatAddresses(addrs []MailAddress) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, FormatAddress(a))
	}
	return strings.Join(parts, ", ")
}
