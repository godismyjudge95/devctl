package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/dnsserver"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

func (s *Server) handleGetServices(w http.ResponseWriter, r *http.Request) {
	states := s.enrichStates(s.poller.CurrentStates())
	writeJSON(w, states)
}

func (s *Server) handleServiceStart(w http.ResponseWriter, r *http.Request) {
	s.runServiceAction(w, r, "start")
}

func (s *Server) handleServiceStop(w http.ResponseWriter, r *http.Request) {
	s.runServiceAction(w, r, "stop")
}

func (s *Server) handleServiceRestart(w http.ResponseWriter, r *http.Request) {
	s.runServiceAction(w, r, "restart")
}

func (s *Server) runServiceAction(w http.ResponseWriter, r *http.Request, action string) {
	id := r.PathValue("id")
	def, ok := s.registry.Get(id)
	if !ok {
		http.Error(w, fmt.Sprintf("service %q not found", id), http.StatusNotFound)
		return
	}

	// Required services cannot be stopped.
	if action == "stop" && def.Required {
		http.Error(w, fmt.Sprintf("service %q is required and cannot be stopped", id), http.StatusForbidden)
		return
	}

	var err error
	switch action {
	case "start":
		err = s.manager.Start(s.serviceDef(r.Context(), def))
	case "stop":
		err = s.manager.Stop(def)
	case "restart":
		err = s.manager.Restart(s.serviceDef(r.Context(), def))
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// When Caddy is started or restarted, wait for the Admin API then push
	// the HTTP server config and sync all vhosts — same as after install.
	if id == "caddy" && (action == "start" || action == "restart") {
		go func() {
			if err := s.caddy.WaitForAdmin(10 * time.Second); err != nil {
				log.Printf("caddy start: admin not ready: %v", err)
				return
			}
			if err := s.caddy.EnsureHTTPServer(s.devctlAddr); err != nil {
				log.Printf("caddy start: ensure http server: %v", err)
			}
			if err := s.siteManager.SyncAll(r.Context()); err != nil {
				log.Printf("caddy start: sync sites: %v", err)
			}
		}()
	}

	// Immediately re-poll so subscribers see the new state without waiting
	// for the next scheduled tick.
	go s.poller.Poll()

	writeJSON(w, map[string]string{"status": "ok"})
}

// handleServiceEvents streams service status updates as Server-Sent Events.
func (s *Server) handleServiceEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send current state immediately.
	sendSSE(w, flusher, "states", s.enrichStates(s.poller.CurrentStates()))

	ch := s.poller.Subscribe()
	defer s.poller.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case update, ok := <-ch:
			if !ok {
				return
			}
			sendSSE(w, flusher, "states", s.enrichStates(update.States))
		}
	}
}

// handleServiceLogs streams a service log file as SSE.
func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def, ok := s.registry.Get(id)
	if !ok {
		http.Error(w, fmt.Sprintf("service %q not found", id), http.StatusNotFound)
		return
	}

	if def.Log == "" {
		http.Error(w, "no log file configured for this service", http.StatusNotFound)
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

	f, err := os.Open(def.Log)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]string{"message": err.Error()})
		return
	}
	defer f.Close()

	// Seek to last 16KB for initial tail.
	if info, err := f.Stat(); err == nil && info.Size() > 16384 {
		f.Seek(-16384, io.SeekEnd)
		// Skip the partial first line.
		buf := make([]byte, 1)
		for {
			_, err := f.Read(buf)
			if err != nil || buf[0] == '\n' {
				break
			}
		}
	}

	buf := make([]byte, 4096)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			n, err := f.Read(buf)
			if n > 0 {
				sendSSE(w, flusher, "log", string(buf[:n]))
			}
			if err != nil && err != io.EOF {
				log.Printf("log stream error: %v", err)
				return
			}
		}
	}
}

// handleClearServiceLogs truncates a service's log file.
func (s *Server) handleClearServiceLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	def, ok := s.registry.Get(id)
	if !ok {
		http.Error(w, fmt.Sprintf("service %q not found", id), http.StatusNotFound)
		return
	}
	if def.Log == "" {
		http.Error(w, "no log file configured for this service", http.StatusNotFound)
		return
	}
	if err := os.Truncate(def.Log, 0); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// handleServiceInstall installs a service, streaming command output as SSE.
// Events:
//
//	output — a chunk of stdout/stderr text (JSON string)
//	done   — {"status":"ok"} on success
//	error  — {"error":"..."} on failure
func (s *Server) handleServiceInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.installers[id]
	if !ok {
		writeError(w, fmt.Sprintf("no installer registered for service %q", id), http.StatusNotFound)
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
	if err := inst.InstallW(r.Context(), pw); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	// Auto-start the service if it is managed (supervised child process).
	if def, ok := s.registry.Get(id); ok && def.Managed {
		if err := s.manager.Start(s.serviceDef(r.Context(), def)); err != nil {
			log.Printf("install: auto-start %s: %v", id, err)
		}
		// For Caddy, wait for the Admin API then push the HTTP server config
		// and sync all vhosts — otherwise sites won't be routed until restart.
		if id == "caddy" {
			go func() {
				if err := s.caddy.WaitForAdmin(10 * time.Second); err != nil {
					log.Printf("install: caddy admin not ready: %v", err)
					return
				}
				if err := s.caddy.EnsureHTTPServer(s.devctlAddr); err != nil {
					log.Printf("install: caddy ensure http server: %v", err)
				}
				if err := s.siteManager.SyncAll(r.Context()); err != nil {
					log.Printf("install: caddy sync sites: %v", err)
				}
			}()
		}
	}

	go s.poller.Poll()
	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})
}

// handleServicePurge removes a service, streaming command output as SSE.
// Events:
//
//	output — a chunk of stdout/stderr text (JSON string)
//	done   — {"status":"ok"} on success
//	error  — {"error":"..."} on failure
func (s *Server) handleServicePurge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Required services cannot be purged.
	if def, ok := s.registry.Get(id); ok && def.Required {
		writeError(w, fmt.Sprintf("service %q is required and cannot be purged", id), http.StatusForbidden)
		return
	}

	inst, ok := s.installers[id]
	if !ok {
		writeError(w, fmt.Sprintf("no installer registered for service %q", id), http.StatusNotFound)
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
	preserveData := r.URL.Query().Get("preserve_data") == "true"
	if err := inst.PurgeW(r.Context(), pw, preserveData); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	go s.poller.Poll()
	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})
}

// handleServiceCredentials reads credentials for a service from its config
// file and returns key=value pairs as a JSON map.
//
// It tries two locations in order:
//  1. $siteHome/sites/server/<id>/config.env  — all pairs returned (server-infra, e.g. meilisearch)
//  2. $siteHome/sites/<id>/.env               — only pairs whose key starts with
//     the upper-cased service ID are returned (site services, e.g. reverb)
//
// Returns 404 if neither file exists.
func (s *Server) handleServiceCredentials(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	def, ok := s.registry.Get(id)
	if !ok {
		http.Error(w, "credentials not found", http.StatusNotFound)
		return
	}

	type candidate struct {
		path      string
		filterPfx string // if non-empty, only keys with this prefix are returned
	}
	upperID := strings.ToUpper(id)

	var candidates []candidate
	if def.CredentialsFile != "" {
		candidates = []candidate{
			{def.CredentialsFile, ""},
		}
	} else {
		sitesDir := filepath.Dir(s.serverRoot)
		candidates = []candidate{
			{filepath.Join(paths.ServiceDir(s.serverRoot, id), "config.env"), ""},
			{filepath.Join(sitesDir, id, ".env"), upperID + "_"},
		}
	}

	var f *os.File
	var filterPfx string
	for _, c := range candidates {
		fh, err := os.Open(c.path)
		if err == nil {
			f = fh
			filterPfx = c.filterPfx
			break
		}
	}
	if f == nil {
		http.Error(w, "credentials not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key == "" {
				continue
			}
			if filterPfx != "" && !strings.HasPrefix(key, filterPfx) {
				continue
			}
			result[key] = val
		}
	}

	writeJSON(w, result)
}

// sseLineWriter forwards writes as SSE "output" events, flushing after each write.
type sseLineWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	event   string
}

func (p *sseLineWriter) Write(b []byte) (int, error) {
	sendSSE(p.w, p.flusher, p.event, string(b))
	return len(b), nil
}

// mailpitDef returns def unchanged for any service that is not mailpit.
// For mailpit it patches ManagedArgs with the current port settings from the DB
// so that start/restart always uses the user-configured ports.
func (s *Server) mailpitDef(ctx context.Context, def services.Definition) services.Definition {
	if def.ID != "mailpit" {
		return def
	}
	httpPort, err := s.queries.GetSetting(ctx, "mailpit_http_port")
	if err != nil || httpPort == "" {
		httpPort = "8025"
	}
	smtpPort, err := s.queries.GetSetting(ctx, "mailpit_smtp_port")
	if err != nil || smtpPort == "" {
		smtpPort = "1025"
	}
	def.ManagedArgs = fmt.Sprintf("--listen 127.0.0.1:%s --smtp 127.0.0.1:%s --database ./data/mailpit.db", httpPort, smtpPort)
	return def
}

// mysqlDef returns def unchanged for any service that is not mysql.
// For mysql it patches ManagedArgs with the current port and bind-address
// settings from the DB so that start/restart always uses the configured values.
func (s *Server) mysqlDef(ctx context.Context, def services.Definition) services.Definition {
	if def.ID != "mysql" {
		return def
	}
	port, err := s.queries.GetSetting(ctx, "mysql_port")
	if err != nil || port == "" {
		port = "3306"
	}
	bindAddr, err := s.queries.GetSetting(ctx, "mysql_bind_address")
	if err != nil || bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	def.ManagedArgs = fmt.Sprintf("--defaults-file=./my.cnf --user=root --port=%s --bind-address=%s", port, bindAddr)
	return def
}

// serviceDef applies any per-service runtime argument patching (e.g. port
// overrides stored in the DB) before the definition is passed to the
// supervisor. Add a new case here whenever a service gains configurable args.
func (s *Server) serviceDef(ctx context.Context, def services.Definition) services.Definition {
	def = s.mailpitDef(ctx, def)
	def = s.mysqlDef(ctx, def)
	def = s.dnsDef(ctx, def)
	return def
}

// ServiceDef is the exported version of serviceDef, used by main.go so the
// auto-start loop at startup applies DB settings (e.g. DNS port, target IP).
func (s *Server) ServiceDef(ctx context.Context, def services.Definition) services.Definition {
	return s.serviceDef(ctx, def)
}

// dnsDef returns def unchanged for any service that is not dns.
// For dns it builds a RunFunc using the current port/target-ip/tld settings
// from the DB so that start/restart always uses the user-configured values.
func (s *Server) dnsDef(ctx context.Context, def services.Definition) services.Definition {
	if def.ID != "dns" {
		return def
	}
	port, err := s.queries.GetSetting(ctx, "dns_port")
	if err != nil || port == "" {
		port = "5354"
	}
	targetIP, err := s.queries.GetSetting(ctx, "dns_target_ip")
	if err != nil || targetIP == "" {
		targetIP = dnsserver.DetectLANIP()
	}
	tldStr, err := s.queries.GetSetting(ctx, "dns_tld")
	if err != nil || tldStr == "" {
		tldStr = ".test"
	}
	// Parse comma-separated TLD list.
	var tlds []string
	for _, t := range strings.Split(tldStr, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tlds = append(tlds, t)
		}
	}
	if len(tlds) == 0 {
		tlds = []string{".test"}
	}

	cfg := dnsserver.Config{
		Port:     port,
		TargetIP: targetIP,
		TLDs:     tlds,
		Upstream: dnsserver.SystemUpstream(),
	}
	def.RunFunc = func(ctx context.Context, logW io.Writer) error {
		return dnsserver.New(cfg).Run(ctx, logW)
	}
	return def
}

// stubHandler returns a 501 Not Implemented for handlers not yet built.
func stubHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// Ensure services import is used (compiler will catch unused imports).
var _ = services.StatusRunning

// ---------------------------------------------------------------------------
// Update helpers
// ---------------------------------------------------------------------------

// SetLatestVersion stores the fetched latest version for a service. Safe for
// concurrent use. Called by the background update-check goroutine in main.go.
func (s *Server) SetLatestVersion(id, version string) {
	s.latestVersionsMu.Lock()
	s.latestVersions[id] = version
	s.latestVersionsMu.Unlock()
}

// enrichStates copies the latest version and update_available flag from the
// in-memory cache into a slice of ServiceState values. Returns the enriched
// slice (new values, original slice is not mutated).
func (s *Server) enrichStates(states []services.ServiceState) []services.ServiceState {
	s.latestVersionsMu.RLock()
	lv := s.latestVersions
	s.latestVersionsMu.RUnlock()

	out := make([]services.ServiceState, len(states))
	for i, st := range states {
		if latest, ok := lv[st.ID]; ok && latest != "" {
			st.LatestVersion = latest
			// update_available when latest != current installed version (strip
			// leading "v" for comparison so "v2.10.0" == "2.10.0" doesn't
			// false-positive, but we do a simple string comparison on normalised
			// values to keep it straightforward).
			current := strings.TrimPrefix(st.InstallVersion, "v")
			latestNorm := strings.TrimPrefix(latest, "v")
			if current != "" && latestNorm != "" && current != latestNorm {
				st.UpdateAvailable = true
			}
		}
		out[i] = st
	}
	return out
}

// handleServiceUpdate performs an update for an installed service, streaming
// command output as SSE. After UpdateW completes the service is restarted.
//
// Events:
//
//	output — a chunk of stdout/stderr text (JSON string)
//	done   — {"status":"ok"} on success
//	error  — {"error":"..."} on failure
func (s *Server) handleServiceUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.installers[id]
	if !ok {
		writeError(w, fmt.Sprintf("no installer registered for service %q", id), http.StatusNotFound)
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
	if err := inst.UpdateW(r.Context(), pw); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	// Restart the service after the binary has been replaced.
	// (Meilisearch's UpdateW already ran the binary directly with --import-dump
	// and the service needs to be started fresh via the supervisor.)
	if def, ok := s.registry.Get(id); ok && def.Managed {
		if err := s.manager.Start(s.serviceDef(r.Context(), def)); err != nil {
			log.Printf("update: restart %s: %v", id, err)
		}
		if id == "caddy" {
			go func() {
				if err := s.caddy.WaitForAdmin(10 * time.Second); err != nil {
					log.Printf("update: caddy admin not ready: %v", err)
					return
				}
				if err := s.caddy.EnsureHTTPServer(s.devctlAddr); err != nil {
					log.Printf("update: caddy ensure http server: %v", err)
				}
				if err := s.siteManager.SyncAll(r.Context()); err != nil {
					log.Printf("update: caddy sync sites: %v", err)
				}
			}()
		}
	}

	go s.poller.Poll()
	sendSSE(w, flusher, "done", map[string]string{"status": "ok"})
}
