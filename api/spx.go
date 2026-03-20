package api

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/danielgormly/devctl/php"
)

// spxMeta mirrors the subset of fields SPX writes to the per-profile .json file.
type spxMeta struct {
	HTTPHost    string  `json:"http_host"`
	HTTPMethod  string  `json:"http_method"`
	HTTPRequest string  `json:"http_request_uri"`
	WallTimeMs  float64 `json:"wall_time_ms"`
	PeakMemory  int64   `json:"peak_memory_usage"`
	CalledFuncs int     `json:"called_function_count"`
	Timestamp   int64   `json:"exec_ts"`
}

// SpxProfile is the list-view representation of a single captured profile.
type SpxProfile struct {
	Key             string  `json:"key"`
	PHPVersion      string  `json:"php_version"`
	Domain          string  `json:"domain"`
	Method          string  `json:"method"`
	URI             string  `json:"uri"`
	WallTimeMs      float64 `json:"wall_time_ms"`
	PeakMemoryBytes int64   `json:"peak_memory_bytes"`
	CalledFuncCount int     `json:"called_func_count"`
	Timestamp       int64   `json:"timestamp"`
}

// SpxFunction is one row in the flat profile table.
type SpxFunction struct {
	Name         string  `json:"name"`
	Calls        int     `json:"calls"`
	InclusiveMs  float64 `json:"inclusive_ms"`
	ExclusiveMs  float64 `json:"exclusive_ms"`
	InclusivePct float64 `json:"inclusive_pct"`
	ExclusivePct float64 `json:"exclusive_pct"`
}

// SpxEvent represents one function call span for the flamegraph / timeline.
type SpxEvent struct {
	Depth      int     `json:"depth"`
	Name       string  `json:"name"`
	StartMs    float64 `json:"start_ms"`
	DurationMs float64 `json:"duration_ms"`
}

// SpxProfileDetail is the full detail response for a single profile.
type SpxProfileDetail struct {
	SpxProfile
	Functions []SpxFunction `json:"functions"`
	Events    []SpxEvent    `json:"events"`
}

// handleGetSpxProfiles lists profiles across all installed PHP versions,
// optionally filtered by ?domain=.
// GET /api/spx/profiles
func (s *Server) handleGetSpxProfiles(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")

	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var profiles []SpxProfile
	for _, v := range versions {
		dir := php.SPXDataDir(v.Version, s.serverRoot)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			writeError(w, fmt.Sprintf("read spx-data for %s: %v", v.Version, err), http.StatusInternalServerError)
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			key := strings.TrimSuffix(e.Name(), ".json")
			p, err := loadProfileMeta(dir, key, v.Version)
			if err != nil {
				// Skip unparseable files.
				continue
			}
			if domain != "" && p.Domain != domain {
				continue
			}
			profiles = append(profiles, p)
		}
	}

	// Sort newest first.
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Timestamp > profiles[j].Timestamp
	})

	if profiles == nil {
		profiles = []SpxProfile{}
	}
	writeJSON(w, profiles)
}

// handleGetSpxProfile returns the full detail for a single profile.
// GET /api/spx/profiles/{key}
func (s *Server) handleGetSpxProfile(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, "key required", http.StatusBadRequest)
		return
	}

	dir, ver, err := s.findSpxProfileDir(key)
	if err != nil {
		writeError(w, "profile not found", http.StatusNotFound)
		return
	}

	meta, err := loadProfileMeta(dir, key, ver)
	if err != nil {
		writeError(w, fmt.Sprintf("read profile: %v", err), http.StatusInternalServerError)
		return
	}

	functions, events, err := parseCallTrace(filepath.Join(dir, key+".txt.gz"), meta.WallTimeMs)
	if err != nil {
		// Return what we have; trace parsing is best-effort.
		writeJSON(w, SpxProfileDetail{SpxProfile: meta})
		return
	}

	writeJSON(w, SpxProfileDetail{
		SpxProfile: meta,
		Functions:  functions,
		Events:     events,
	})
}

// handleDeleteSpxProfile deletes a single profile (both .json and .txt.gz).
// DELETE /api/spx/profiles/{key}
func (s *Server) handleDeleteSpxProfile(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, "key required", http.StatusBadRequest)
		return
	}

	dir, _, err := s.findSpxProfileDir(key)
	if err != nil {
		writeError(w, "profile not found", http.StatusNotFound)
		return
	}

	_ = os.Remove(filepath.Join(dir, key+".json"))
	_ = os.Remove(filepath.Join(dir, key+".txt.gz"))
	w.WriteHeader(http.StatusNoContent)
}

// handleClearSpxProfiles deletes all profiles, optionally scoped to ?domain=.
// DELETE /api/spx/profiles
func (s *Server) handleClearSpxProfiles(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")

	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, v := range versions {
		dir := php.SPXDataDir(v.Version, s.serverRoot)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			key := strings.TrimSuffix(e.Name(), ".json")
			if domain != "" {
				p, err := loadProfileMeta(dir, key, v.Version)
				if err != nil || p.Domain != domain {
					continue
				}
			}
			_ = os.Remove(filepath.Join(dir, key+".json"))
			_ = os.Remove(filepath.Join(dir, key+".txt.gz"))
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

// findSpxProfileDir scans all PHP version spx-data directories to locate the
// one that contains {key}.json. Returns the dir path and PHP version.
func (s *Server) findSpxProfileDir(key string) (dir, ver string, err error) {
	versions, listErr := php.InstalledVersions(s.serverRoot)
	if listErr != nil {
		return "", "", listErr
	}
	for _, v := range versions {
		d := php.SPXDataDir(v.Version, s.serverRoot)
		if _, statErr := os.Stat(filepath.Join(d, key+".json")); statErr == nil {
			return d, v.Version, nil
		}
	}
	return "", "", fmt.Errorf("profile %s not found", key)
}

// loadProfileMeta reads {dir}/{key}.json and returns a SpxProfile.
func loadProfileMeta(dir, key, phpVersion string) (SpxProfile, error) {
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return SpxProfile{}, err
	}
	var m spxMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return SpxProfile{}, err
	}
	// Strip port from host if present.
	host := m.HTTPHost
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return SpxProfile{
		Key:             key,
		PHPVersion:      phpVersion,
		Domain:          host,
		Method:          m.HTTPMethod,
		URI:             m.HTTPRequest,
		WallTimeMs:      m.WallTimeMs,
		PeakMemoryBytes: m.PeakMemory,
		CalledFuncCount: m.CalledFuncs,
		Timestamp:       m.Timestamp,
	}, nil
}

// readSPXSections decompresses a SPX .txt.gz trace file and returns the
// function-name table and the raw event lines. Both slices reference the same
// underlying string allocation so callers should not hold both for long.
//
// SPX full reporter format:
//
//	[events]
//	{func_idx} {is_enter} {wt_us} [{extra} ...]
//
//	[functions]
//	one function name per line, 0-based
func readSPXSections(path string) (funcNames []string, eventLines []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, err
	}
	defer gz.Close()

	raw, err := io.ReadAll(gz)
	if err != nil {
		return nil, nil, err
	}

	// Find section start line numbers.
	eventsStart, functionsStart := -1, -1
	pos, lineNum := 0, 0
	for pos < len(raw) {
		end := pos
		for end < len(raw) && raw[end] != '\n' {
			end++
		}
		line := strings.TrimSpace(string(raw[pos:end]))
		switch line {
		case "[events]":
			eventsStart = lineNum + 1
		case "[functions]":
			functionsStart = lineNum + 1
		}
		lineNum++
		pos = end + 1
	}

	if eventsStart < 0 || functionsStart < 0 {
		return nil, nil, fmt.Errorf("unrecognised SPX trace format")
	}

	lines := strings.Split(string(raw), "\n")
	raw = nil // allow GC

	// [functions] section: one name per line.
	funcNames = make([]string, 0, 8192)
	for _, l := range lines[functionsStart:] {
		name := strings.TrimSpace(l)
		if name != "" {
			funcNames = append(funcNames, name)
		}
	}

	// [events] section ends just before the "[functions]" header line.
	eventsEnd := functionsStart - 1
	if eventsEnd > len(lines) {
		eventsEnd = len(lines)
	}
	eventLines = lines[eventsStart:eventsEnd]

	return funcNames, eventLines, nil
}

// parseCallTrace decompresses and parses a SPX .txt.gz call trace file.
// To avoid OOM on very large traces we cap the flamegraph/timeline events
// slice at maxFlameEvents; the flat profile has no cap.
const maxFlameEvents = 5000

func parseCallTrace(path string, totalMs float64) ([]SpxFunction, []SpxEvent, error) {
	funcNames, eventLines, err := readSPXSections(path)
	if err != nil {
		return nil, nil, err
	}

	nameFor := func(idx int) string {
		if idx >= 0 && idx < len(funcNames) {
			return funcNames[idx]
		}
		return fmt.Sprintf("func#%d", idx)
	}

	// Process enter/exit events.
	type stackFrame struct {
		name    string
		depth   int
		enterUs int64
	}
	type funcAgg struct {
		calls       int
		inclusiveUs int64
		exclusiveUs int64
	}

	stack := make([]stackFrame, 0, 128)
	agg := make(map[string]*funcAgg, 512)
	events := make([]SpxEvent, 0, maxFlameEvents)

	// childDurations[depth] accumulates total child durations for the frame at depth.
	childDurations := make(map[int]int64, 128)

	for _, line := range eventLines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		funcIdx, err1 := strconv.Atoi(parts[0])
		isEnter, err2 := strconv.Atoi(parts[1])
		wtUs, err3 := strconv.ParseInt(parts[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}

		name := nameFor(funcIdx)
		depth := len(stack) // depth = current stack depth before push/pop

		switch isEnter {
		case 1: // entry
			childDurations[depth] = 0
			stack = append(stack, stackFrame{name: name, depth: depth, enterUs: wtUs})

		case 0: // exit
			if len(stack) == 0 {
				continue
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			frameDepth := frame.depth

			durationUs := wtUs - frame.enterUs
			if durationUs < 0 {
				durationUs = 0
			}

			// Exclusive = duration minus direct children's total time.
			exclusiveUs := durationUs - childDurations[frameDepth]
			if exclusiveUs < 0 {
				exclusiveUs = 0
			}

			// Add this call's duration to the parent's child accumulator.
			if frameDepth > 0 {
				childDurations[frameDepth-1] += durationUs
			}
			childDurations[frameDepth] = 0

			// Aggregate flat profile (always — no cap).
			fa, ok := agg[frame.name]
			if !ok {
				fa = &funcAgg{}
				agg[frame.name] = fa
			}
			fa.calls++
			fa.inclusiveUs += durationUs
			fa.exclusiveUs += exclusiveUs

			// Flamegraph / timeline events — capped to avoid OOM.
			if len(events) < maxFlameEvents {
				startMs := float64(frame.enterUs) / 1000.0
				durationMs := float64(durationUs) / 1000.0
				events = append(events, SpxEvent{
					Depth:      frameDepth,
					Name:       frame.name,
					StartMs:    startMs,
					DurationMs: durationMs,
				})
			}
		}
	}

	// Build flat profile list sorted by exclusive time descending.
	totalUs := totalMs * 1000.0
	functions := make([]SpxFunction, 0, len(agg))
	for name, a := range agg {
		inclMs := float64(a.inclusiveUs) / 1000.0
		exclMs := float64(a.exclusiveUs) / 1000.0
		var inclPct, exclPct float64
		if totalUs > 0 {
			inclPct = float64(a.inclusiveUs) / totalUs * 100.0
			exclPct = float64(a.exclusiveUs) / totalUs * 100.0
		}
		functions = append(functions, SpxFunction{
			Name:         name,
			Calls:        a.calls,
			InclusiveMs:  inclMs,
			ExclusiveMs:  exclMs,
			InclusivePct: inclPct,
			ExclusivePct: exclPct,
		})
	}
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].ExclusiveMs > functions[j].ExclusiveMs
	})

	return functions, events, nil
}
