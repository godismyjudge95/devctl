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
	HTTPHost    string  `json:"_http_host"`
	HTTPMethod  string  `json:"_http_method"`
	HTTPRequest string  `json:"_http_request_uri"`
	WallTimeMs  float64 `json:"wall_time_ms"`
	PeakMemory  int64   `json:"peak_memory_usage"`
	CalledFuncs int     `json:"called_func_count"`
	Timestamp   int64   `json:"timestamp"`
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

// parseCallTrace decompresses and parses a SPX .txt.gz call trace file.
//
// SPX full reporter format:
//
//	Header lines (start with #):
//	  # spx-version {n}
//	  # php-version {v}
//	  # enabled-metrics wt [zm ...]
//	  # func {idx} {file}:{line} {func_name}
//
//	Data lines (tab-separated):
//	  {+|-} {depth} {func_idx} {wt_us} [{zm_bytes} ...]
//
// wt_us is the cumulative wall-time in microseconds from request start.
func parseCallTrace(path string, totalMs float64) ([]SpxFunction, []SpxEvent, error) {
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

	lines := strings.Split(string(raw), "\n")

	// Pass 1: build function index.
	funcNames := map[int]string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "# func ") {
			continue
		}
		// "# func {idx} {file}:{line} {func_name}"
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		funcNames[idx] = parts[4]
	}

	// Pass 2: process enter/exit events.
	type stackFrame struct {
		funcIdx int
		enterUs int64
	}
	type funcAgg struct {
		calls       int
		inclusiveUs int64
		exclusiveUs int64
	}

	stack := []stackFrame{}
	agg := map[string]*funcAgg{}
	var events []SpxEvent

	// Track exclusive time: subtract child durations from parent.
	childDurations := map[int]int64{} // depth → total child us at that depth

	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		eventType := parts[0]
		depth, err1 := strconv.Atoi(parts[1])
		funcIdx, err2 := strconv.Atoi(parts[2])
		wtUs, err3 := strconv.ParseInt(parts[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}

		name := funcNames[funcIdx]
		if name == "" {
			name = fmt.Sprintf("func#%d", funcIdx)
		}

		switch eventType {
		case "+":
			stack = append(stack, stackFrame{funcIdx: funcIdx, enterUs: wtUs})
			childDurations[depth] = 0

		case "-":
			if len(stack) == 0 {
				continue
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			durationUs := wtUs - frame.enterUs
			if durationUs < 0 {
				durationUs = 0
			}

			// Exclusive = duration minus direct children's total time.
			exclusiveUs := durationUs - childDurations[depth]
			if exclusiveUs < 0 {
				exclusiveUs = 0
			}

			// Add this duration to parent's child accumulator.
			if depth > 0 {
				childDurations[depth-1] += durationUs
			}
			childDurations[depth] = 0

			// Aggregate flat profile.
			if _, ok := agg[name]; !ok {
				agg[name] = &funcAgg{}
			}
			agg[name].calls++
			agg[name].inclusiveUs += durationUs
			agg[name].exclusiveUs += exclusiveUs

			// Flamegraph / timeline event.
			startMs := float64(frame.enterUs) / 1000.0
			durationMs := float64(durationUs) / 1000.0
			events = append(events, SpxEvent{
				Depth:      depth,
				Name:       name,
				StartMs:    startMs,
				DurationMs: durationMs,
			})
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
