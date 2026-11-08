package inspector

// Rules are the problem thresholds. A zero threshold turns its rule off.
type Rules struct {
	NPlusOneMin   int     // same normalized SQL from the same caller, this many times
	SlowQueryMs   float64 // one query at or over this duration
	SlowRequestMs float64 // the unit at or over this duration
}

// DefaultRules match the inspector_* setting defaults.
var DefaultRules = Rules{NPlusOneMin: 3, SlowQueryMs: 100, SlowRequestMs: 1000}

// ProblemKind names a problem rule.
type ProblemKind string

const (
	NPlusOne    ProblemKind = "n_plus_one"
	Duplicate   ProblemKind = "duplicate"
	SlowQuery   ProblemKind = "slow_query"
	SlowRequest ProblemKind = "slow_request"
)

// Problem is one flagged finding in a unit.
type Problem struct {
	Kind   ProblemKind `json:"kind"`
	SQL    string      `json:"sql,omitempty"` // normalized for n_plus_one, as sent otherwise
	Caller *Caller     `json:"caller,omitempty"`
	Count  int         `json:"count,omitempty"`
	Ms     float64     `json:"ms"`              // group total, or the query or unit duration
	Event  *int        `json:"event,omitempty"` // index of the first matching event
}

type nPlusOneKey struct {
	sql, file string
	line      int
}

type duplicateKey struct{ sql, bindings string }

type group struct {
	first, count int
	ms           float64
	members      []int
}

// Analyze applies rules to u. It is pure, so a threshold change re-flags
// units that are already in the ring.
func Analyze(u *Unit, rules Rules) []Problem {
	nGroups, nOrder := map[nPlusOneKey]*group{}, []nPlusOneKey{}
	dGroups, dOrder := map[duplicateKey]*group{}, []duplicateKey{}
	var slow []Problem

	for i, ev := range u.Events {
		q := ev.Query
		if q == nil {
			continue
		}
		if ev.Caller != nil {
			k := nPlusOneKey{q.Normalized, ev.Caller.File, ev.Caller.Line}
			if nGroups[k] == nil {
				nGroups[k] = &group{first: i}
				nOrder = append(nOrder, k)
			}
			nGroups[k].add(i, q.Ms)
		}
		dk := duplicateKey{q.SQL, q.Bindings}
		if dGroups[dk] == nil {
			dGroups[dk] = &group{first: i}
			dOrder = append(dOrder, dk)
		}
		dGroups[dk].add(i, q.Ms)
		if rules.SlowQueryMs > 0 && q.Ms >= rules.SlowQueryMs {
			slow = append(slow, Problem{Kind: SlowQuery, SQL: q.SQL, Caller: ev.Caller, Ms: q.Ms, Event: ptr(i)})
		}
	}

	var out []Problem
	inNPlusOne := map[int]bool{}
	for _, k := range nOrder {
		g := nGroups[k]
		if rules.NPlusOneMin <= 0 || g.count < rules.NPlusOneMin {
			continue
		}
		for _, i := range g.members {
			inNPlusOne[i] = true
		}
		out = append(out, Problem{Kind: NPlusOne, SQL: k.sql, Caller: u.Events[g.first].Caller, Count: g.count, Ms: g.ms, Event: ptr(g.first)})
	}
	for _, k := range dOrder {
		g := dGroups[k]
		if g.count < 2 || allIn(g.members, inNPlusOne) {
			continue
		}
		out = append(out, Problem{Kind: Duplicate, SQL: k.sql, Caller: u.Events[g.first].Caller, Count: g.count, Ms: g.ms, Event: ptr(g.first)})
	}
	out = append(out, slow...)
	if rules.SlowRequestMs > 0 && u.Final && u.Meta.DurationMs >= rules.SlowRequestMs {
		out = append(out, Problem{Kind: SlowRequest, Ms: u.Meta.DurationMs})
	}
	if out == nil {
		out = []Problem{}
	}
	return out
}

func (g *group) add(i int, ms float64) {
	g.count++
	g.ms += ms
	g.members = append(g.members, i)
}

func allIn(members []int, set map[int]bool) bool {
	for _, i := range members {
		if !set[i] {
			return false
		}
	}
	return true
}

func ptr(i int) *int { return &i }
