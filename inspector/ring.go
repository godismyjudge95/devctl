// Package inspector keeps the request inspector units that the PHP agent
// (php/inspector.php) sends over the dump TCP port, in memory, newest last.
package inspector

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Default ring limits: a unit count and a byte budget of received JSON.
const (
	DefaultMaxUnits = 200
	DefaultMaxBytes = 64 << 20
)

// Meta describes a unit: an HTTP request, an artisan command, or a queued job.
// Each frame carries the full meta; fields present in a newer frame win.
type Meta struct {
	Kind       string  `json:"kind,omitempty"`
	Method     string  `json:"method,omitempty"`
	URL        string  `json:"url,omitempty"`
	Host       string  `json:"host,omitempty"`
	Route      string  `json:"route,omitempty"`
	Action     string  `json:"action,omitempty"`
	Status     int     `json:"status,omitempty"`
	Command    string  `json:"command,omitempty"`
	Artisan    string  `json:"artisan,omitempty"`
	Job        string  `json:"job,omitempty"`
	Queue      string  `json:"queue,omitempty"`
	Connection string  `json:"connection,omitempty"`
	JobID      any     `json:"job_id,omitempty"`
	Attempt    int     `json:"attempt,omitempty"`
	JobStatus  string  `json:"job_status,omitempty"`
	Parent     string  `json:"parent,omitempty"`
	PID        int     `json:"pid,omitempty"`
	PHP        string  `json:"php,omitempty"`
	SAPI       string  `json:"sapi,omitempty"`
	StartedAt  float64 `json:"started_at,omitempty"`
	DurationMs float64 `json:"duration_ms,omitempty"`
	MemoryPeak int64   `json:"memory_peak,omitempty"`
	Dropped    int     `json:"dropped,omitempty"`
}

// Caller is the app file and line that caused an event.
type Caller struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Query is the part of a query event that analysis reads.
type Query struct {
	SQL        string
	Normalized string
	Bindings   string // raw JSON, compared as text for duplicates
	Ms         float64
}

// Event is one recorded event. Raw is the event as the agent sent it; the
// API passes it through, so a new PHP field needs no Go change.
type Event struct {
	Kind   string
	T      float64
	Caller *Caller
	Query  *Query
	Raw    json.RawMessage
}

// Unit is one request, job, or command.
type Unit struct {
	ID        string
	Meta      Meta
	Events    []Event
	Dumps     []json.RawMessage
	Final     bool
	Bytes     int
	UpdatedAt time.Time
	metaSeq   int // seq of the frame whose meta was merged last
}

// Summary is a unit without its events: the list view and the live feed.
type Summary struct {
	ID        string         `json:"id"`
	Meta      Meta           `json:"meta"`
	Final     bool           `json:"final"`
	UpdatedAt time.Time      `json:"updated_at"`
	Counts    map[string]int `json:"counts"`
	QueryMs   float64        `json:"query_ms"`
	Problems  []Problem      `json:"problems"`
}

// Detail is a unit with its events and linked dumps.
type Detail struct {
	Summary
	Events []json.RawMessage `json:"events"`
	Dumps  []json.RawMessage `json:"dumps"`
}

// Filter narrows List.
type Filter struct {
	Site     string // meta.host
	Kind     string // meta.kind
	Problems bool   // only units with at least one problem
	Limit    int
}

// Ring holds the newest units within a unit count and a byte budget.
type Ring struct {
	mu       sync.Mutex
	order    []string // unit ids, oldest first
	units    map[string]*Unit
	bytes    int
	maxUnits int
	maxBytes int
	rules    Rules
}

// NewRing creates a Ring with the given limits and DefaultRules.
func NewRing(maxUnits, maxBytes int) *Ring {
	return &Ring{units: map[string]*Unit{}, maxUnits: maxUnits, maxBytes: maxBytes, rules: DefaultRules}
}

// SetRules changes the problem thresholds. Units already in the ring are
// flagged again on the next read.
func (r *Ring) SetRules(rules Rules) {
	r.mu.Lock()
	r.rules = rules
	r.mu.Unlock()
}

type frame struct {
	V      int               `json:"v"`
	Type   string            `json:"type"`
	RID    string            `json:"rid"`
	Seq    int               `json:"seq"`
	Final  bool              `json:"final"`
	Meta   json.RawMessage   `json:"meta"`
	Events []json.RawMessage `json:"events"`
}

type eventFields struct {
	Kind     string          `json:"kind"`
	T        float64         `json:"t"`
	Caller   *Caller         `json:"caller"`
	SQL      string          `json:"sql"`
	Bindings json.RawMessage `json:"bindings"`
	Ms       *float64        `json:"ms"`
}

// ErrBadRID is returned for a unit id that is not 1 to 64 lowercase hex characters.
var ErrBadRID = errors.New("inspector: bad unit id")

// ValidRID reports whether id can be a unit id. Ids appear in URLs.
func ValidRID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if c := id[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Ingest adds one JSON frame line from the agent and returns the updated
// unit's summary.
func (r *Ring) Ingest(line []byte) (Summary, error) {
	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Summary{}, fmt.Errorf("inspector: frame: %w", err)
	}
	if f.Type != "unit" {
		return Summary{}, fmt.Errorf("inspector: unknown frame type %q", f.Type)
	}
	if !ValidRID(f.RID) {
		return Summary{}, ErrBadRID
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.unit(f.RID)
	if len(f.Meta) > 0 && f.Seq >= u.metaSeq {
		m := u.Meta
		if err := json.Unmarshal(f.Meta, &m); err == nil {
			u.Meta = m
			u.metaSeq = f.Seq
		}
	}
	u.Final = u.Final || f.Final

	// One unit may use at most the whole budget; later events are dropped.
	if u.Bytes+len(line) <= r.maxBytes {
		outOfOrder := len(u.Events) > 0 && f.Seq < u.metaSeq
		for _, raw := range f.Events {
			if ev, ok := parseEvent(raw); ok {
				u.Events = append(u.Events, ev)
			}
		}
		if outOfOrder {
			sort.SliceStable(u.Events, func(i, j int) bool { return u.Events[i].T < u.Events[j].T })
		}
		u.Bytes += len(line)
		r.bytes += len(line)
	}
	u.UpdatedAt = time.Now()
	r.evict(u.ID)
	return summarize(u, r.rules), nil
}

// LinkDump attaches a dump (the stored row as JSON) to unit rid. A dump can
// arrive before the unit's first frame, so it creates the unit.
func (r *Ring) LinkDump(rid string, dump json.RawMessage) (Summary, error) {
	if !ValidRID(rid) {
		return Summary{}, ErrBadRID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.unit(rid)
	u.Dumps = append(u.Dumps, dump)
	u.Bytes += len(dump)
	r.bytes += len(dump)
	u.UpdatedAt = time.Now()
	r.evict(u.ID)
	return summarize(u, r.rules), nil
}

func parseEvent(raw json.RawMessage) (Event, bool) {
	var f eventFields
	if err := json.Unmarshal(raw, &f); err != nil || f.Kind == "" {
		return Event{}, false
	}
	ev := Event{Kind: f.Kind, T: f.T, Caller: f.Caller, Raw: raw}
	if f.Kind == "query" {
		q := &Query{SQL: f.SQL, Normalized: Normalize(f.SQL), Bindings: string(f.Bindings)}
		if f.Ms != nil {
			q.Ms = *f.Ms
		}
		ev.Query = q
	}
	return ev, true
}

// unit returns the unit with id, created at the newest end when missing.
func (r *Ring) unit(id string) *Unit {
	if u, ok := r.units[id]; ok {
		return u
	}
	u := &Unit{ID: id, metaSeq: -1}
	r.units[id] = u
	r.order = append(r.order, id)
	return u
}

// evict drops the oldest units until both limits hold. It never drops keep,
// the unit that just changed.
func (r *Ring) evict(keep string) {
	for i := 0; i < len(r.order) && (len(r.order) > r.maxUnits || r.bytes > r.maxBytes); {
		id := r.order[i]
		if id == keep {
			i++
			continue
		}
		r.order = append(r.order[:i], r.order[i+1:]...)
		r.bytes -= r.units[id].Bytes
		delete(r.units, id)
	}
}

// List returns summaries newest first.
func (r *Ring) List(f Filter) []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Summary{}
	for i := len(r.order) - 1; i >= 0; i-- {
		u := r.units[r.order[i]]
		if f.Site != "" && u.Meta.Host != f.Site || f.Kind != "" && u.Meta.Kind != f.Kind {
			continue
		}
		s := summarize(u, r.rules)
		if f.Problems && len(s.Problems) == 0 {
			continue
		}
		out = append(out, s)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out
}

// Get returns one unit with its events.
func (r *Ring) Get(id string) (Detail, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.units[id]
	if !ok {
		return Detail{}, false
	}
	d := Detail{Summary: summarize(u, r.rules), Events: make([]json.RawMessage, len(u.Events)), Dumps: append([]json.RawMessage{}, u.Dumps...)}
	for i, ev := range u.Events {
		d.Events[i] = ev.Raw
	}
	return d, true
}

// Clear removes every unit.
func (r *Ring) Clear() {
	r.mu.Lock()
	r.order = nil
	r.units = map[string]*Unit{}
	r.bytes = 0
	r.mu.Unlock()
}

func summarize(u *Unit, rules Rules) Summary {
	s := Summary{ID: u.ID, Meta: u.Meta, Final: u.Final, UpdatedAt: u.UpdatedAt, Counts: map[string]int{}, Problems: Analyze(u, rules)}
	for _, ev := range u.Events {
		s.Counts[ev.Kind]++
		if ev.Query != nil {
			s.QueryMs += ev.Query.Ms
		}
	}
	if len(u.Dumps) > 0 {
		s.Counts["dump"] = len(u.Dumps)
	}
	return s
}
