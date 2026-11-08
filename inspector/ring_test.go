package inspector

import (
	"encoding/json"
	"fmt"
	"testing"
)

type testEvent map[string]any

func query(sql string, bindings []any, ms float64, file string, line int) testEvent {
	return testEvent{"kind": "query", "t": 1.0, "sql": sql, "bindings": bindings, "ms": ms, "caller": map[string]any{"file": file, "line": line}}
}

func frameLine(t *testing.T, rid string, seq int, final bool, meta map[string]any, events ...testEvent) []byte {
	t.Helper()
	if events == nil {
		events = []testEvent{}
	}
	b, err := json.Marshal(map[string]any{"v": 1, "type": "unit", "rid": rid, "seq": seq, "final": final, "meta": meta, "events": events})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ingest(t *testing.T, r *Ring, line []byte) Summary {
	t.Helper()
	s, err := r.Ingest(line)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return s
}

func kinds(ps []Problem) []ProblemKind {
	out := []ProblemKind{}
	for _, p := range ps {
		out = append(out, p.Kind)
	}
	return out
}

func unitOf(t *testing.T, events ...testEvent) *Unit {
	t.Helper()
	r := NewRing(10, DefaultMaxBytes)
	ingest(t, r, frameLine(t, "a1", 0, true, map[string]any{"kind": "http"}, events...))
	return r.units["a1"]
}

func TestAnalyze_NPlusOne(t *testing.T) {
	u := unitOf(t,
		query("select * from users where id = 1", nil, 1, "/app/C.php", 10),
		query("select * from users where id = 2", nil, 2, "/app/C.php", 10),
		query("select * from users where id = 3", nil, 3, "/app/C.php", 10),
	)
	ps := Analyze(u, DefaultRules)
	if len(ps) != 1 || ps[0].Kind != NPlusOne {
		t.Fatalf("problems = %v, want one n_plus_one", kinds(ps))
	}
	p := ps[0]
	if p.Count != 3 || p.Ms != 6 || p.SQL != "select * from users where id = ?" || *p.Event != 0 || p.Caller.Line != 10 {
		t.Errorf("n_plus_one = %+v", p)
	}
	if ps := Analyze(u, Rules{NPlusOneMin: 4}); len(ps) != 0 {
		t.Errorf("min 4: problems = %v, want none", kinds(ps))
	}
}

func TestAnalyze_NPlusOneNeedsSameCaller(t *testing.T) {
	u := unitOf(t,
		query("select * from users where id = 1", nil, 1, "/app/C.php", 10),
		query("select * from users where id = 2", nil, 1, "/app/C.php", 11),
		query("select * from users where id = 3", nil, 1, "/app/D.php", 10),
	)
	if ps := Analyze(u, DefaultRules); len(ps) != 0 {
		t.Errorf("problems = %v, want none", kinds(ps))
	}
}

func TestAnalyze_Duplicate(t *testing.T) {
	u := unitOf(t,
		query("select * from settings where k = ?", []any{"a"}, 1, "/app/A.php", 1),
		query("select * from settings where k = ?", []any{"b"}, 1, "/app/A.php", 2),
		query("select * from settings where k = ?", []any{"a"}, 1, "/app/B.php", 3),
	)
	ps := Analyze(u, DefaultRules)
	if len(ps) != 1 || ps[0].Kind != Duplicate || ps[0].Count != 2 || *ps[0].Event != 0 {
		t.Fatalf("problems = %+v, want one duplicate of 2 at event 0", ps)
	}
}

func TestAnalyze_DuplicateInsideNPlusOneShowsOnlyNPlusOne(t *testing.T) {
	q := query("select * from roles where id = ?", []any{1}, 1, "/app/C.php", 5)
	ps := Analyze(unitOf(t, q, q, q), DefaultRules)
	if got := kinds(ps); len(got) != 1 || got[0] != NPlusOne {
		t.Errorf("problems = %v, want [n_plus_one]", got)
	}
}

func TestAnalyze_SlowQueryAndSlowRequest(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	ingest(t, r, frameLine(t, "b1", 0, false, map[string]any{"kind": "http"},
		query("select 1", nil, 5, "/app/A.php", 1),
		query("select 2", nil, 150, "/app/A.php", 2),
	))
	if got := kinds(Analyze(r.units["b1"], DefaultRules)); len(got) != 1 || got[0] != SlowQuery {
		t.Fatalf("before final: problems = %v, want [slow_query]", got)
	}
	ps := Analyze(r.units["b1"], DefaultRules)
	if *ps[0].Event != 1 || ps[0].Ms != 150 {
		t.Errorf("slow_query = %+v", ps[0])
	}

	ingest(t, r, frameLine(t, "b1", 1, true, map[string]any{"kind": "http", "duration_ms": 1500}))
	if got := kinds(Analyze(r.units["b1"], DefaultRules)); len(got) != 2 || got[1] != SlowRequest {
		t.Errorf("after final: problems = %v, want [slow_query slow_request]", got)
	}
	if got := Analyze(r.units["b1"], Rules{}); len(got) != 0 {
		t.Errorf("zero rules: problems = %v, want none", kinds(got))
	}
}

func TestRing_FramesOutOfSeqOrder(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	late := testEvent{"kind": "log", "t": 9.0, "level": "info", "message": "late"}
	early := testEvent{"kind": "log", "t": 1.0, "level": "info", "message": "early"}
	ingest(t, r, frameLine(t, "c1", 1, true, map[string]any{"kind": "http", "route": "final.route", "duration_ms": 12.5}, late))
	s := ingest(t, r, frameLine(t, "c1", 0, false, map[string]any{"kind": "http", "route": "early.route"}, early))

	if !s.Final || s.Meta.Route != "final.route" || s.Meta.DurationMs != 12.5 {
		t.Errorf("meta = %+v final=%v, want the seq 1 meta to win", s.Meta, s.Final)
	}
	d, _ := r.Get("c1")
	var first map[string]any
	_ = json.Unmarshal(d.Events[0], &first)
	if len(d.Events) != 2 || first["message"] != "early" {
		t.Errorf("events not ordered by t: %s", d.Events)
	}
}

func TestRing_DumpBeforeUnit(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	if _, err := r.LinkDump("d1", json.RawMessage(`{"id":7}`)); err != nil {
		t.Fatal(err)
	}
	s := ingest(t, r, frameLine(t, "d1", 0, true, map[string]any{"kind": "command"}, testEvent{"kind": "log", "t": 1.0}))
	if s.Counts["dump"] != 1 || s.Counts["log"] != 1 || s.Meta.Kind != "command" {
		t.Errorf("summary = %+v", s)
	}
	d, ok := r.Get("d1")
	if !ok || len(d.Dumps) != 1 || string(d.Dumps[0]) != `{"id":7}` {
		t.Errorf("detail dumps = %s", d.Dumps)
	}
}

func TestRing_UnitWithoutFinalFrame(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	ingest(t, r, frameLine(t, "e1", 0, false, map[string]any{"kind": "http", "host": "shop.test"}, testEvent{"kind": "view", "t": 1.0, "name": "home"}))
	list := r.List(Filter{})
	if len(list) != 1 || list[0].Final || list[0].Counts["view"] != 1 {
		t.Errorf("list = %+v", list)
	}
}

func TestRing_EvictsByCount(t *testing.T) {
	r := NewRing(2, DefaultMaxBytes)
	for _, id := range []string{"f1", "f2", "f3"} {
		ingest(t, r, frameLine(t, id, 0, true, map[string]any{"kind": "http"}))
	}
	list := r.List(Filter{})
	if len(list) != 2 || list[0].ID != "f3" || list[1].ID != "f2" {
		t.Errorf("ids = %v, want [f3 f2]", ids(list))
	}
	if _, ok := r.Get("f1"); ok {
		t.Error("f1 still in the ring")
	}
}

func TestRing_EvictsByBytes(t *testing.T) {
	line := frameLine(t, "a0", 0, true, map[string]any{"kind": "http"})
	r := NewRing(100, len(line)*2+10)
	for _, id := range []string{"a1", "a2", "a3"} {
		ingest(t, r, frameLine(t, id, 0, true, map[string]any{"kind": "http"}))
	}
	if got := ids(r.List(Filter{})); len(got) != 2 || got[0] != "a3" || got[1] != "a2" {
		t.Errorf("ids = %v, want [a3 a2]", got)
	}
	if r.bytes > r.maxBytes {
		t.Errorf("ring holds %d bytes, budget %d", r.bytes, r.maxBytes)
	}
}

func TestRing_RejectsBadFrames(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	for _, line := range []string{
		`{"v":1,"type":"unit","rid":"../etc","seq":0}`,
		`{"v":1,"type":"unit","rid":"","seq":0}`,
		`{"v":1,"type":"other","rid":"ab","seq":0}`,
		`{not json`,
	} {
		if _, err := r.Ingest([]byte(line)); err == nil {
			t.Errorf("Ingest(%s): want an error", line)
		}
	}
	if _, err := r.LinkDump("XYZ", json.RawMessage(`{}`)); err == nil {
		t.Error("LinkDump with a bad id: want an error")
	}
	if len(r.List(Filter{})) != 0 {
		t.Error("a bad frame created a unit")
	}
}

func TestRing_ListFilters(t *testing.T) {
	r := NewRing(10, DefaultMaxBytes)
	n1 := []testEvent{
		query("select 1", nil, 1, "/a.php", 1), query("select 2", nil, 1, "/a.php", 1), query("select 3", nil, 1, "/a.php", 1),
	}
	ingest(t, r, frameLine(t, "aa", 0, true, map[string]any{"kind": "http", "host": "one.test"}, n1...))
	ingest(t, r, frameLine(t, "bb", 0, true, map[string]any{"kind": "http", "host": "two.test"}))
	ingest(t, r, frameLine(t, "cc", 0, true, map[string]any{"kind": "command"}))

	for _, tc := range []struct {
		f    Filter
		want string
	}{
		{Filter{}, "[cc bb aa]"},
		{Filter{Site: "one.test"}, "[aa]"},
		{Filter{Kind: "command"}, "[cc]"},
		{Filter{Problems: true}, "[aa]"},
		{Filter{Limit: 2}, "[cc bb]"},
	} {
		if got := fmt.Sprint(ids(r.List(tc.f))); got != tc.want {
			t.Errorf("List(%+v) = %s, want %s", tc.f, got, tc.want)
		}
	}

	r.SetRules(Rules{NPlusOneMin: 5})
	if got := r.List(Filter{Problems: true}); len(got) != 0 {
		t.Errorf("after SetRules: problem units = %v, want none", ids(got))
	}

	r.Clear()
	if got := r.List(Filter{}); len(got) != 0 {
		t.Errorf("after Clear: %v", ids(got))
	}
}

func ids(list []Summary) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.ID)
	}
	return out
}
