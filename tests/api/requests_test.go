//go:build integration

package apitest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type inspectorProblem struct {
	Kind   string `json:"kind"`
	SQL    string `json:"sql"`
	Count  int    `json:"count"`
	Event  *int   `json:"event"`
	Caller *struct {
		File string `json:"file"`
		Line int    `json:"line"`
	} `json:"caller"`
}

type inspectorUnit struct {
	ID   string `json:"id"`
	Meta struct {
		Kind    string `json:"kind"`
		Host    string `json:"host"`
		Command string `json:"command"`
	} `json:"meta"`
	Final    bool               `json:"final"`
	Counts   map[string]int     `json:"counts"`
	Problems []inspectorProblem `json:"problems"`
	Events   []map[string]any   `json:"events"`
	Dumps    []map[string]any   `json:"dumps"`
}

func (u inspectorUnit) problem(kind string) *inspectorProblem {
	for i := range u.Problems {
		if u.Problems[i].Kind == kind {
			return &u.Problems[i]
		}
	}
	return nil
}

func newRID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// sendToDumpPort writes lines to the dump TCP listener, as PHP does.
func sendToDumpPort(t *testing.T, lines ...string) {
	t.Helper()
	port := decodeJSON[map[string]string](t, httpGet(t, "/api/settings/resolved"))["dump_tcp_port"]
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second)
	if err != nil {
		t.Fatalf("dial dump port %s: %v", port, err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
}

func unitFrame(t *testing.T, rid string, seq int, final bool, meta map[string]any, events ...map[string]any) string {
	t.Helper()
	if events == nil {
		events = []map[string]any{}
	}
	b, err := json.Marshal(map[string]any{"v": 1, "type": "unit", "rid": rid, "seq": seq, "final": final, "meta": meta, "events": events})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func queryEvent(sql string, binding any, line int) map[string]any {
	return map[string]any{
		"kind": "query", "t": 1.5, "sql": sql, "bindings": []any{binding}, "ms": 0.4, "conn": "mysql",
		"caller": map[string]any{"file": "/home/u/sites/shop/app/Http/Controllers/OrderController.php", "line": line},
	}
}

// waitForUnit polls GET /api/requests/{id} until ok(unit) holds.
func waitForUnit(t *testing.T, rid string, ok func(inspectorUnit) bool) inspectorUnit {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last inspectorUnit
	for time.Now().Before(deadline) {
		body, status := httpGetRaw(t, "/api/requests/"+rid)
		if status == 200 {
			last = decodeJSON[inspectorUnit](t, body)
			if ok(last) {
				return last
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("unit %s never reached the expected state; last: %+v", rid, last)
	return last
}

func setSetting(t *testing.T, key, value string) {
	t.Helper()
	if body, status := httpPut(t, "/api/settings", map[string]string{key: value}); status != 200 {
		t.Fatalf("PUT %s=%s: %d %s", key, value, status, body)
	}
}

func listHas(t *testing.T, path, rid string) bool {
	t.Helper()
	for _, u := range decodeJSON[[]inspectorUnit](t, httpGet(t, path)) {
		if u.ID == rid {
			return true
		}
	}
	return false
}

// TestRequests_SyntheticFrameFlagsNPlusOne sends a frame the way the PHP agent
// does and reads the unit back with its N+1 flag.
func TestRequests_SyntheticFrameFlagsNPlusOne(t *testing.T) {
	rid := newRID(t)
	meta := map[string]any{"kind": "http", "method": "GET", "url": "https://inspector-api.test/orders", "host": "inspector-api.test"}
	sendToDumpPort(t,
		unitFrame(t, rid, 0, false, meta,
			queryEvent("select * from `users` where `users`.`id` = ? limit 1", 1, 31),
			queryEvent("select * from `users` where `users`.`id` = ? limit 1", 2, 31),
		),
		unitFrame(t, rid, 1, true, map[string]any{"kind": "http", "host": "inspector-api.test", "status": 200, "duration_ms": 42.5},
			queryEvent("select * from `users` where `users`.`id` = ? limit 1", 3, 31),
			map[string]any{"kind": "log", "t": 2.0, "level": "info", "message": "done"},
		),
	)

	u := waitForUnit(t, rid, func(u inspectorUnit) bool { return u.Final && len(u.Events) == 4 })
	p := u.problem("n_plus_one")
	if p == nil || p.Count != 3 || p.Caller == nil || p.Caller.Line != 31 || p.Event == nil || *p.Event != 0 {
		t.Fatalf("n_plus_one = %+v, want count 3 at OrderController.php:31, event 0", p)
	}
	if u.Counts["query"] != 3 || u.Counts["log"] != 1 {
		t.Errorf("counts = %v", u.Counts)
	}
	if u.Events[0]["conn"] != "mysql" {
		t.Errorf("event fields Go does not read must pass through: %v", u.Events[0])
	}
	if !listHas(t, "/api/requests?site=inspector-api.test&problems=true", rid) {
		t.Error("GET /api/requests?site=…&problems=true does not list the unit")
	}
	if listHas(t, "/api/requests?kind=job", rid) {
		t.Error("GET /api/requests?kind=job lists an http unit")
	}
}

// TestRequests_DumpLinkedByRID: a dump tagged with a unit id shows in the
// unit, even when it arrives before the unit's first frame.
func TestRequests_DumpLinkedByRID(t *testing.T) {
	rid := newRID(t)
	marker := "inspector-dump-" + rid
	payload, _ := json.Marshal(map[string]any{
		"timestamp": float64(time.Now().Unix()),
		"source":    map[string]any{"file": "/app/routes/web.php", "line": 7, "name": "web.php"},
		"host":      "inspector-api.test",
		"nodes":     []any{map[string]any{"type": "string", "value": marker, "length": len(marker)}},
		"rid":       rid,
	})
	sendToDumpPort(t, base64.StdEncoding.EncodeToString(payload))
	sendToDumpPort(t, unitFrame(t, rid, 0, true, map[string]any{"kind": "http", "host": "inspector-api.test"}))

	u := waitForUnit(t, rid, func(u inspectorUnit) bool { return u.Final && len(u.Dumps) == 1 })
	if b, _ := json.Marshal(u.Dumps[0]); !strings.Contains(string(b), marker) {
		t.Errorf("linked dump = %s, want the %s dump", b, marker)
	}
	waitForDump(t, marker)
}

// TestRequests_RuleSettingsReflagUnits: thresholds apply at read time.
func TestRequests_RuleSettingsReflagUnits(t *testing.T) {
	t.Cleanup(func() { setSetting(t, "inspector_n_plus_one_min", "3") })
	rid := newRID(t)
	sendToDumpPort(t, unitFrame(t, rid, 0, true, map[string]any{"kind": "command"},
		queryEvent("select * from posts where id = ?", 1, 9),
		queryEvent("select * from posts where id = ?", 2, 9),
		queryEvent("select * from posts where id = ?", 3, 9),
	))
	waitForUnit(t, rid, func(u inspectorUnit) bool { return u.problem("n_plus_one") != nil })

	setSetting(t, "inspector_n_plus_one_min", "4")
	if u := waitForUnit(t, rid, func(inspectorUnit) bool { return true }); u.problem("n_plus_one") != nil {
		t.Errorf("min 4: unit still flagged: %+v", u.Problems)
	}

	for key, bad := range map[string]string{
		"inspector_n_plus_one_min":  "1",
		"inspector_slow_query_ms":   "-5",
		"inspector_slow_request_ms": "slow",
		"inspector_enabled":         "maybe",
		"editor_url":                "javascript:alert('{file}')",
	} {
		if _, status := httpPut(t, "/api/settings", map[string]string{key: bad}); status != 400 {
			t.Errorf("PUT %s=%s: status %d, want 400", key, bad, status)
		}
	}
	resolved := decodeJSON[map[string]string](t, httpGet(t, "/api/settings/resolved"))
	if resolved["inspector_enabled"] != "true" || resolved["editor_url"] != "vscode://file/{file}:{line}" {
		t.Errorf("resolved inspector defaults = enabled %q, editor_url %q", resolved["inspector_enabled"], resolved["editor_url"])
	}
}

// TestRequests_InspectorEnabledWritesAgent: the setting writes or deletes the
// agent that prepend.php loads, and the prepend sends to dump_tcp_port.
func TestRequests_InspectorEnabledWritesAgent(t *testing.T) {
	agent := filepath.Join(phpTestServerRoot(), "devctl", "inspector.php")
	t.Cleanup(func() { setSetting(t, "inspector_enabled", "true") })

	setSetting(t, "inspector_enabled", "false")
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Errorf("inspector_enabled=false: %s still exists (%v)", agent, err)
	}
	setSetting(t, "inspector_enabled", "true")
	if _, err := os.Stat(agent); err != nil {
		t.Errorf("inspector_enabled=true: %v", err)
	}

	port := decodeJSON[map[string]string](t, httpGet(t, "/api/settings/resolved"))["dump_tcp_port"]
	prepend, err := os.ReadFile(filepath.Join(phpTestServerRoot(), "devctl", "prepend.php"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "'tcp://127.0.0.1:" + port + "'; // devctl:dump_addr"; !strings.Contains(string(prepend), want) {
		t.Errorf("prepend.php does not contain %s", want)
	}
}

func TestRequests_ClearAndNotFound(t *testing.T) {
	rid := newRID(t)
	sendToDumpPort(t, unitFrame(t, rid, 0, true, map[string]any{"kind": "command"}))
	waitForUnit(t, rid, func(inspectorUnit) bool { return true })

	if _, status := httpDelete(t, "/api/requests"); status != 204 {
		t.Fatalf("DELETE /api/requests: status %d, want 204", status)
	}
	if got := decodeJSON[[]inspectorUnit](t, httpGet(t, "/api/requests")); len(got) != 0 {
		t.Errorf("after clear: %d units", len(got))
	}
	httpGetStatus(t, "/api/requests/"+rid, 404)
}

// runArtisan runs script, saved as dir/artisan, with the devctl php wrapper
// and the nonce as its only argument: the agent records a command unit for a
// script named artisan, with meta.command = the arguments.
func runArtisan(t *testing.T, phpBin, dir, script, nonce string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, "artisan"), script)
	cmd := exec.Command(phpBin, filepath.Join(dir, "artisan"), nonce)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("php artisan: %v\n%s", err, out)
	}
	return string(out)
}

// waitForCommandUnit finds the command unit whose arguments are nonce.
func waitForCommandUnit(t *testing.T, nonce string) inspectorUnit {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, u := range decodeJSON[[]inspectorUnit](t, httpGet(t, "/api/requests?kind=command")) {
			if u.Meta.Command == nonce && u.Final {
				return decodeJSON[inspectorUnit](t, httpGet(t, "/api/requests/"+u.ID))
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no finished command unit for `artisan %s` reached devctl", nonce)
	return inspectorUnit{}
}

func phpEval(t *testing.T, phpBin, code string) string {
	t.Helper()
	out, err := exec.Command(phpBin, "-r", code).CombinedOutput()
	if err != nil {
		t.Fatalf("php -r %q: %v\n%s", code, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestRequests_CapturedFromPHPCLI_PDO runs an N+1 loop over plain PDO
// (sqlite) through the devctl php wrapper. The agent's PDOStatement::execute
// hook needs PHP 8.2+ and the opentelemetry extension.
func TestRequests_CapturedFromPHPCLI_PDO(t *testing.T) {
	phpBin, _ := requireRealPHP(t)
	if v, _ := strconv.Atoi(phpEval(t, phpBin, "echo PHP_VERSION_ID;")); v < 80200 {
		t.Skipf("PHP version id %d: internal function hooks need 8.2+", v)
	}
	if got := phpEval(t, phpBin, "echo extension_loaded('opentelemetry') ? 'yes' : 'no';"); got != "yes" {
		t.Fatalf("the devctl PHP build has no opentelemetry extension; the request inspector depends on it")
	}
	if got := phpEval(t, phpBin, "echo in_array('sqlite', PDO::getAvailableDrivers(), true) ? 'yes' : 'no';"); got != "yes" {
		t.Skip("no PDO sqlite driver in this PHP build; TestRequests_CapturedFromPHPCLI_Laravel covers the agent without a database")
	}

	dir := t.TempDir()
	nonce := "pdo-" + newRID(t)
	script := `<?php
$pdo = new PDO('sqlite::memory:', null, null, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
$pdo->exec('create table users (id integer primary key, name varchar(20))');
$pdo->exec("insert into users (id, name) values (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd')");
foreach ([1, 2, 3, 4] as $id) {
    $st = $pdo->prepare('select * from users where id = ?');
    $st->execute([$id]);
}
php_dd('` + nonce + `');
echo "workload done\n";
`
	if out := runArtisan(t, phpBin, dir, script, nonce); !strings.Contains(out, "workload done") {
		t.Fatalf("workload output changed by the agent:\n%s", out)
	}

	u := waitForCommandUnit(t, nonce)
	p := u.problem("n_plus_one")
	if p == nil || p.Count != 4 || p.SQL != "select * from users where id = ?" {
		t.Fatalf("n_plus_one = %+v, want 4 x select * from users where id = ?; problems %+v", p, u.Problems)
	}
	if p.Caller == nil || p.Caller.File != filepath.Join(dir, "artisan") || p.Caller.Line != 7 {
		t.Errorf("caller = %+v, want %s:7", p.Caller, filepath.Join(dir, "artisan"))
	}
	if len(u.Dumps) != 1 {
		t.Errorf("unit has %d dumps, want the php_dd() dump linked by rid", len(u.Dumps))
	}
}

// laravelStubs are the framework classes the agent hooks, in vendor/ so the
// caller rule skips them.
const laravelStubs = `<?php
namespace Illuminate\Events {
    class Dispatcher
    {
        private array $listeners = [];
        public function listen($event, $listener) { $this->listeners[$event][] = $listener; }
        public function dispatch($event, $payload = [])
        {
            if (is_object($event)) {
                $payload = [$event];
                $event = get_class($event);
            }
            foreach ($this->listeners[$event] ?? [] as $l) {
                $l(...$payload);
            }
        }
    }
}
namespace Illuminate\Foundation {
    class Application
    {
        private $events = null;
        private array $after = [];
        public function __construct() {}
        public function resolved($key) { return $this->events !== null; }
        public function afterResolving($key, $cb) { $this->after[] = $cb; }
        public function make($key)
        {
            if ($this->events === null) {
                $this->events = new \Illuminate\Events\Dispatcher();
                foreach ($this->after as $cb) {
                    $cb($this->events, $this);
                }
            }
            return $this->events;
        }
    }
}
namespace Illuminate\Database\Events {
    class QueryExecuted
    {
        public function __construct(public $sql, public $bindings, public $time, public $connection, public $connectionName) {}
    }
}
namespace Illuminate\Log\Events {
    class MessageLogged
    {
        public function __construct(public $level, public $message, public $context = []) {}
    }
}
namespace {
    class StubConnection { public function getDriverName() { return 'sqlite'; } }
}
`

// TestRequests_CapturedFromPHPCLI_Laravel runs a Laravel-shaped workload: the
// agent attaches through its Application::__construct hook (PHP 8.1+) and
// records QueryExecuted and MessageLogged events. No database driver needed.
func TestRequests_CapturedFromPHPCLI_Laravel(t *testing.T) {
	phpBin, _ := requireRealPHP(t)
	if got := phpEval(t, phpBin, "echo extension_loaded('opentelemetry') ? 'yes' : 'no';"); got != "yes" {
		t.Fatalf("the devctl PHP build has no opentelemetry extension; the request inspector depends on it")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vendor", "laravel-stub.php"), laravelStubs)
	nonce := "laravel-" + newRID(t)
	script := `<?php
require __DIR__ . '/vendor/laravel-stub.php';
$app = new Illuminate\Foundation\Application();
$events = $app->make('events');
foreach ([1, 2, 3] as $id) {
    $events->dispatch(new Illuminate\Database\Events\QueryExecuted('select * from users where id = ?', [$id], 0.42, new StubConnection(), 'sqlite'));
}
$events->dispatch(new Illuminate\Log\Events\MessageLogged('error', 'boom', ['exception' => new RuntimeException('boom')]));
echo "workload done\n";
`
	if out := runArtisan(t, phpBin, dir, script, nonce); !strings.Contains(out, "workload done") {
		t.Fatalf("workload output changed by the agent:\n%s", out)
	}

	u := waitForCommandUnit(t, nonce)
	p := u.problem("n_plus_one")
	if p == nil || p.Count != 3 {
		t.Fatalf("n_plus_one = %+v, want count 3; problems %+v, counts %v", p, u.Problems, u.Counts)
	}
	if p.Caller == nil || p.Caller.File != filepath.Join(dir, "artisan") || p.Caller.Line != 6 {
		t.Errorf("caller = %+v, want %s:6 (vendor/ frames skipped)", p.Caller, filepath.Join(dir, "artisan"))
	}
	if u.Counts["log"] != 1 || u.Counts["exception"] != 1 {
		t.Errorf("counts = %v, want 1 log and 1 exception", u.Counts)
	}
	for _, ev := range u.Events {
		if ev["kind"] == "exception" && ev["class"] != "RuntimeException" {
			t.Errorf("exception event = %v", ev)
		}
	}
}
