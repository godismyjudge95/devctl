package api

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

// speedscopeFile is the top-level speedscope JSON format (v1).
// See https://www.speedscope.app/file-format-schema.json
type speedscopeFile struct {
	Schema   string              `json:"$schema"`
	Shared   speedscopeShared    `json:"shared"`
	Profiles []speedscopeProfile `json:"profiles"`
	Name     string              `json:"name"`
	Exporter string              `json:"exporter"`
}

type speedscopeShared struct {
	Frames []speedscopeFrame `json:"frames"`
}

type speedscopeFrame struct {
	Name string `json:"name"`
}

// speedscopeProfile covers both sampled and evented; we only use sampled.
type speedscopeProfile struct {
	Type       string    `json:"type"`
	Name       string    `json:"name"`
	Unit       string    `json:"unit"`
	StartValue float64   `json:"startValue"`
	EndValue   float64   `json:"endValue"`
	Samples    [][]int   `json:"samples"`
	Weights    []float64 `json:"weights"`
}

// handleGetSpxSpeedscope converts a SPX trace to speedscope SampledProfile JSON
// and returns it gzip-compressed so the browser can load it directly.
//
// GET /api/spx/profiles/{key}/speedscope
func (s *Server) handleGetSpxSpeedscope(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	dir, _, err := s.findSpxProfileDir(key)
	if err != nil {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}

	meta, err := loadProfileMeta(dir, key, "")
	if err != nil {
		http.Error(w, fmt.Sprintf("read profile: %v", err), http.StatusInternalServerError)
		return
	}

	sf, err := buildSpeedscopeFile(filepath.Join(dir, key+".txt.gz"), meta)
	if err != nil {
		http.Error(w, fmt.Sprintf("build speedscope: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Cache-Control", "no-store")

	gz := gzip.NewWriter(w)
	defer gz.Close()

	enc := json.NewEncoder(gz)
	if err := enc.Encode(sf); err != nil {
		// Headers already sent; nothing useful we can do.
		return
	}
}

// buildSpeedscopeFile parses the SPX .txt.gz trace and converts it to a
// speedscope SampledProfile. Each completed call frame emits one sample whose
// stack is the full path from root to that frame and whose weight is the
// frame's exclusive wall-time in microseconds.
//
// Using SampledProfile (rather than EventedProfile) keeps the JSON small:
// instead of 2 × N events for N calls, we emit one entry per unique stack
// path (typically a few thousand for a Laravel request) with aggregated
// weights.
func buildSpeedscopeFile(tracePath string, meta SpxProfile) (*speedscopeFile, error) {
	// --- parse the raw trace ---
	funcNames, eventLines, err := readSPXSections(tracePath)
	if err != nil {
		return nil, err
	}

	nameFor := func(idx int) string {
		if idx >= 0 && idx < len(funcNames) {
			return funcNames[idx]
		}
		return fmt.Sprintf("func#%d", idx)
	}

	// --- frame registry (deduplicates function names → integer index) ---
	frameIndex := make(map[string]int, len(funcNames))
	frames := make([]speedscopeFrame, 0, len(funcNames))

	getFrameIdx := func(name string) int {
		if idx, ok := frameIndex[name]; ok {
			return idx
		}
		idx := len(frames)
		frameIndex[name] = idx
		frames = append(frames, speedscopeFrame{Name: name})
		return idx
	}

	// --- sample aggregation ---
	// key: string encoding of the stack (frame indices joined), value: index into samples/weights
	type stackKey = string
	sampleIndex := make(map[stackKey]int, 4096)
	samples := make([][]int, 0, 4096)
	weights := make([]float64, 0, 4096)

	// stackBuf holds the current call stack as frame indices (root first).
	stackBuf := make([]int, 0, 128)

	// stackKeyBuf reuses a []byte to build the map key without allocating.
	stackKeyBuf := make([]byte, 0, 512)

	// childDurations[depth] accumulates direct-child exclusive time for the
	// frame currently at that depth, so we can compute exclusive time on exit.
	type frame struct {
		frameIdx int
		enterUs  int64
	}
	callStack := make([]frame, 0, 128)
	childDuration := make([]int64, 0, 128) // parallel to callStack

	addSample := func(exclusiveUs int64) {
		if exclusiveUs <= 0 || len(stackBuf) == 0 {
			return
		}
		// Build a cheap string key from the frame-index slice.
		stackKeyBuf = stackKeyBuf[:0]
		for _, fi := range stackBuf {
			stackKeyBuf = fmt.Appendf(stackKeyBuf, "%d,", fi)
		}
		key := stackKey(stackKeyBuf)

		if idx, ok := sampleIndex[key]; ok {
			weights[idx] += float64(exclusiveUs)
		} else {
			// Copy stackBuf — it will be mutated later.
			s := make([]int, len(stackBuf))
			copy(s, stackBuf)
			sampleIndex[key] = len(samples)
			samples = append(samples, s)
			weights = append(weights, float64(exclusiveUs))
		}
	}

	// --- process events ---
	for _, line := range eventLines {
		if line == "" {
			continue
		}
		// Fast manual parse: "funcIdx isEnter wtUs [extra...]"
		var funcIdx, isEnter int
		var wtUs int64
		n, _ := fmt.Sscanf(line, "%d %d %d", &funcIdx, &isEnter, &wtUs)
		if n < 3 {
			continue
		}

		switch isEnter {
		case 1: // entry
			fi := getFrameIdx(nameFor(funcIdx))
			stackBuf = append(stackBuf, fi)
			callStack = append(callStack, frame{frameIdx: fi, enterUs: wtUs})
			childDuration = append(childDuration, 0)

		case 0: // exit
			if len(callStack) == 0 {
				continue
			}
			top := callStack[len(callStack)-1]
			childUs := childDuration[len(childDuration)-1]

			callStack = callStack[:len(callStack)-1]
			childDuration = childDuration[:len(childDuration)-1]
			stackBuf = stackBuf[:len(stackBuf)-1]

			durationUs := wtUs - top.enterUs
			if durationUs < 0 {
				durationUs = 0
			}
			exclusiveUs := durationUs - childUs
			if exclusiveUs < 0 {
				exclusiveUs = 0
			}

			// Credit this frame's total duration to the parent's child accumulator.
			if len(childDuration) > 0 {
				childDuration[len(childDuration)-1] += durationUs
			}

			// Emit a sample at this stack path weighted by exclusive time.
			addSample(exclusiveUs)
		}
	}

	totalUs := meta.WallTimeMs * 1000.0

	sf := &speedscopeFile{
		Schema:   "https://www.speedscope.app/file-format-schema.json",
		Name:     fmt.Sprintf("%s %s", meta.Method, meta.URI),
		Exporter: "devctl",
		Shared:   speedscopeShared{Frames: frames},
		Profiles: []speedscopeProfile{
			{
				Type:       "sampled",
				Name:       fmt.Sprintf("%s %s", meta.Method, meta.URI),
				Unit:       "microseconds",
				StartValue: 0,
				EndValue:   totalUs,
				Samples:    samples,
				Weights:    weights,
			},
		},
	}

	return sf, nil
}
