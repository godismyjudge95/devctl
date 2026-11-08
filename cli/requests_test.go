package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/danielgormly/devctl/inspector"
)

const sampleUnits = `[{"id":"aa11","meta":{"kind":"http","method":"GET","url":"https://shop.test/orders","host":"shop.test","status":200,"duration_ms":182.4},"final":true,
"counts":{"query":4},"query_ms":3,"problems":[{"kind":"n_plus_one","sql":"select * from users where id = ?","caller":{"file":"/app/C.php","line":31},"count":3,"ms":2.5,"event":0}]},
{"id":"bb22","meta":{"kind":"command","artisan":"app:sync"},"final":true,"counts":{},"query_ms":0,"problems":[]}]`

func TestRequestsList_SendsFiltersToTheAPI(t *testing.T) {
	c, seen := fakeDaemon(t, map[string]string{"/api/requests": sampleUnits})
	units, err := c.ListRequests(requestFilter{site: "shop.test", problems: true, limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := seen(); len(got) != 1 || got[0] != "GET /api/requests?limit=5&problems=true&site=shop.test" {
		t.Errorf("requests = %q", got)
	}
	if len(units) != 2 || units[0].Problems[0].Kind != inspector.NPlusOne {
		t.Errorf("units = %+v", units)
	}
}

func TestRequestsProblems_FlattensWithTheUnit(t *testing.T) {
	var units []inspector.Summary
	if err := json.Unmarshal([]byte(sampleUnits), &units); err != nil {
		t.Fatal(err)
	}
	found := flattenProblems(units)
	if len(found) != 1 || found[0].ID != "aa11" || found[0].What != "GET https://shop.test/orders" || found[0].Count != 3 {
		t.Fatalf("found = %+v", found)
	}
	b, _ := json.Marshal(found[0])
	for _, key := range []string{`"id":"aa11"`, `"kind":"n_plus_one"`, `"caller":{"file":"/app/C.php","line":31}`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("JSON %s lacks %s", b, key)
		}
	}
	if got := describeUnit(units[1].Meta); got != "artisan app:sync" {
		t.Errorf("describeUnit(command) = %q", got)
	}
	if got := problemTags(units[0].Problems); got != "N+1 x1" {
		t.Errorf("problemTags = %q", got)
	}
}

// TestRequestsWatch_PrintsFinishedUnitsAsNDJSON: only finished units that
// match the filter print, one JSON object per line.
func TestRequestsWatch_PrintsFinishedUnitsAsNDJSON(t *testing.T) {
	msgs := []string{
		`{"id":"aa11","meta":{"kind":"http","host":"shop.test"},"final":false,"problems":[]}`,
		`{"id":"aa11","meta":{"kind":"http","host":"shop.test","status":200},"final":true,"problems":[]}`,
		`{"id":"cc33","meta":{"kind":"http","host":"other.test"},"final":true,"problems":[]}`,
		`{"id":"dd44","meta":{"kind":"command"},"final":true,"problems":[]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/requests" {
			http.NotFound(w, r)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, m := range msgs {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(m))
		}
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.WatchRequests(ctx, requestFilter{site: "shop.test"}, &out, true)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1:\n%s", len(lines), out.String())
	}
	var u inspector.Summary
	if err := json.Unmarshal([]byte(lines[0]), &u); err != nil || u.ID != "aa11" || !u.Final {
		t.Errorf("line = %s (%v)", lines[0], err)
	}
}
