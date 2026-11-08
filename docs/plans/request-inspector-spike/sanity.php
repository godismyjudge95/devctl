<?php
/**
 * Behavioral check for the request inspector agent. Run it with a devctl
 * static PHP 8.1+ binary, and pass the agent that devctl installed (default:
 * php/inspector.php in this repository):
 *
 *   ~/ddev/sites/server/php/8.4/php sanity.php ~/ddev/sites/server/devctl/inspector.php
 *
 * Optional: SANITY_DSN, SANITY_USER, SANITY_PASS when the build has no PDO sqlite driver.
 * It starts a TCP listener, runs two workloads named "artisan" with the agent as
 * auto_prepend_file, and checks the frames the agent sends. Last line: RESULT: PASS|FAIL.
 */

$inspector = $argv[1] ?? __DIR__ . '/../../../php/inspector.php';
if (!is_file($inspector)) {
    fwrite(STDERR, "no inspector agent at $inspector\n");
    exit(2);
}
$server = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
if ($server === false) {
    fwrite(STDERR, "listen: $errstr\n");
    exit(2);
}
$addr = 'tcp://' . stream_socket_get_name($server, false);
$drivers = PDO::getAvailableDrivers();
$dsn = getenv('SANITY_DSN') ?: 'sqlite::memory:';
$hasDb = getenv('SANITY_DSN') !== false || in_array('sqlite', $drivers, true);

echo 'PHP ' . PHP_VERSION . ' ' . PHP_OS_FAMILY . "\n";
echo 'opentelemetry loaded: ' . (extension_loaded('opentelemetry') ? 'yes ' . phpversion('opentelemetry') : 'no') . "\n";
echo 'PDO drivers: ' . implode(',', $drivers) . "\n";
echo "listener: $addr\n\n";

$failed = 0;
function check(string $name, bool $ok, string $detail = ''): void
{
    global $failed;
    if (!$ok) {
        $failed++;
    }
    echo ($ok ? 'PASS ' : 'FAIL ') . $name . ($detail !== '' ? "  ($detail)" : '') . "\n";
}

function skip(string $name, string $why): void
{
    echo "SKIP $name  ($why)\n";
}

function marker_line(string $src, string $marker): int
{
    foreach (explode("\n", $src) as $i => $line) {
        if (strpos($line, $marker) !== false) {
            return $i + 1;
        }
    }
    return -1;
}

/** @return array{frames: array, output: string, dir: string} */
function run_workload(string $name, string $artisan, array $vendorFiles, $server, string $addr, string $inspector): array
{
    $dir = sys_get_temp_dir() . '/devctl-inspector-sanity-' . $name . '-' . getmypid();
    @mkdir($dir . '/vendor', 0777, true);
    file_put_contents("$dir/artisan", $artisan);
    foreach ($vendorFiles as $file => $code) {
        file_put_contents("$dir/vendor/$file", $code);
    }
    $env = getenv();
    $env['DEVCTL_INSPECTOR_ADDR'] = $addr;
    $proc = proc_open([PHP_BINARY, '-d', 'auto_prepend_file=' . $inspector, "$dir/artisan"], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, $dir, $env);
    stream_set_blocking($pipes[1], false);
    stream_set_blocking($pipes[2], false);

    $frames = [];
    $output = '';
    $deadline = microtime(true) + 20;
    $exitedAt = null;
    while (microtime(true) < $deadline) {
        $read = [$server];
        $w = $e = null;
        if (stream_select($read, $w, $e, 0, 200000)) {
            $conn = stream_socket_accept($server, 1);
            $data = $conn ? stream_get_contents($conn) : '';
            if ($conn) {
                fclose($conn);
            }
            foreach (explode("\n", $data) as $line) {
                if ($line !== '') {
                    $frames[] = json_decode($line, true);
                }
            }
        }
        $output .= stream_get_contents($pipes[1]) . stream_get_contents($pipes[2]);
        if (!proc_get_status($proc)['running']) {
            $exitedAt ??= microtime(true);
            if (microtime(true) - $exitedAt > 1.0) {
                break;
            }
        }
    }
    proc_close($proc);
    return ['frames' => $frames, 'output' => $output, 'dir' => $dir];
}

function events_of(array $frames, string $kind): array
{
    $out = [];
    foreach ($frames as $f) {
        foreach ($f['events'] ?? [] as $e) {
            if (($e['kind'] ?? '') === $kind) {
                $out[] = $e;
            }
        }
    }
    return $out;
}

function check_unit(string $label, array $frames, string $output): void
{
    check("$label: agent sent frames", count($frames) > 0, count($frames) . ' frame(s); workload output: ' . trim($output));
    if ($frames === []) {
        return;
    }
    $rids = array_unique(array_column($frames, 'rid'));
    $last = end($frames);
    check("$label: one unit id", count($rids) === 1, implode(',', $rids));
    check("$label: last frame is final", ($last['final'] ?? false) === true);
    check("$label: meta.kind is command", ($last['meta']['kind'] ?? '') === 'command');
    check("$label: duration_ms present", is_numeric($last['meta']['duration_ms'] ?? null));
}

// ---- Workload 1: plain PDO + curl (generic hooks, PHP 8.2+) ----

$generic = <<<'PHP'
<?php
$pdo = new PDO(getenv('SANITY_DSN') ?: 'sqlite::memory:', getenv('SANITY_USER') ?: null, getenv('SANITY_PASS') ?: null, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
$pdo->exec('drop table if exists sanity_users');
$pdo->exec('create table sanity_users (id integer primary key, name varchar(20))');
$pdo->exec("insert into sanity_users (id, name) values (1, 'a'), (2, 'b'), (3, 'c'), (4, 'd')");
foreach ([1, 2, 3, 4] as $id) {
    $st = $pdo->prepare('select * from sanity_users where id = ?');
    $st->execute([$id]); // @n1
}
$st = $pdo->prepare('select * from sanity_users where name = :name');
$st->bindValue(':name', 'b');
$st->execute(); // @bind
$ch = curl_init('http://127.0.0.1:1/');
curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
curl_exec($ch); // @curl
echo "workload done\n";
PHP;

if (PHP_VERSION_ID < 80200) {
    skip('generic PDO/curl hooks', 'internal functions are observable only on PHP 8.2+');
} elseif (!$hasDb) {
    skip('generic PDO/curl hooks', 'no PDO sqlite driver; set SANITY_DSN/SANITY_USER/SANITY_PASS');
} else {
    $r = run_workload('generic', $generic, [], $server, $addr, $inspector);
    check_unit('generic', $r['frames'], $r['output']);
    $n1Line = marker_line($generic, '@n1');
    $queries = events_of($r['frames'], 'query');
    $n1 = array_values(array_filter($queries, static fn ($q) => $q['sql'] === 'select * from sanity_users where id = ?' && ($q['caller']['line'] ?? 0) === $n1Line));
    check('generic: 4 identical queries from the same caller line', count($n1) === 4, count($n1) . " at line $n1Line");
    check('generic: execute() params captured as bindings', array_column($n1, 'bindings') === [[1], [2], [3], [4]], json_encode(array_column($n1, 'bindings')));
    check('generic: caller file is the workload', ($n1[0]['caller']['file'] ?? '') === $r['dir'] . '/artisan', $n1[0]['caller']['file'] ?? 'none');
    check('generic: query duration measured', is_numeric($n1[0]['ms'] ?? null));
    $bind = array_values(array_filter($queries, static fn ($q) => ($q['caller']['line'] ?? 0) === marker_line($generic, '@bind')));
    check('generic: bindValue() captured', ($bind[0]['bindings'] ?? null) === [':name' => 'b'], json_encode($bind[0]['bindings'] ?? null));
    $http = events_of($r['frames'], 'http');
    check('generic: failed curl_exec recorded with error', isset($http[0]['error']) && strpos($http[0]['url'] ?? '', '127.0.0.1:1') !== false, json_encode($http[0] ?? null));
    check('generic: workload result unchanged', strpos($r['output'], 'workload done') !== false);
}

echo "\n";

// ---- Workload 2: Laravel adapter against stub framework classes ----

$stubs = <<<'PHP'
<?php
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
            foreach ($this->listeners as $pattern => $listeners) {
                $wild = strpos($pattern, '*') !== false;
                if ($pattern !== $event && !($wild && fnmatch($pattern, $event))) {
                    continue;
                }
                foreach ($listeners as $l) {
                    $wild ? $l($event, $payload) : $l(...$payload);
                }
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
namespace Illuminate\Cache\Events {
    class CacheHit
    {
        public function __construct(public $storeName, public $key, public $value) {}
    }
}
namespace {
    class StubConnection { public function getDriverName() { return 'sqlite'; } }
    class StubView
    {
        public function name() { return 'welcome'; }
        public function getPath() { return '/app/resources/views/welcome.blade.php'; }
    }
}
PHP;

$laravel = <<<'PHP'
<?php
require __DIR__ . '/vendor/laravel-stub.php';
$app = new Illuminate\Foundation\Application();
$events = $app->make('events');
foreach ([1, 2, 3] as $id) {
    $events->dispatch(new Illuminate\Database\Events\QueryExecuted('select * from users where id = ?', [$id], 0.42, new StubConnection(), 'sqlite')); // @lq
}
$events->dispatch(new Illuminate\Cache\Events\CacheHit('file', 'settings', 1));
$events->dispatch('composing: welcome', [new StubView()]);
$events->dispatch(new Illuminate\Log\Events\MessageLogged('error', 'boom', ['exception' => new RuntimeException('boom')]));
if (in_array('sqlite', PDO::getAvailableDrivers(), true)) {
    (new PDO('sqlite::memory:'))->query('select 1'); // must not be captured: Laravel owns SQL capture now
}
echo "workload done\n";
PHP;

$r = run_workload('laravel', $laravel, ['laravel-stub.php' => $stubs], $server, $addr, $inspector);
check_unit('laravel', $r['frames'], $r['output']);
$lqLine = marker_line($laravel, '@lq');
$queries = events_of($r['frames'], 'query');
$lq = array_values(array_filter($queries, static fn ($q) => ($q['caller']['line'] ?? 0) === $lqLine));
check('laravel: Application::__construct hook attached listeners (3 QueryExecuted)', count($lq) === 3, count($lq) . ' query event(s)');
check('laravel: caller skips vendor/ frames', ($lq[0]['caller']['file'] ?? '') === $r['dir'] . '/artisan', $lq[0]['caller']['file'] ?? 'none');
check('laravel: bindings, time, connection, driver', ($lq[0]['bindings'] ?? null) === [1] && ($lq[0]['ms'] ?? null) === 0.42 && ($lq[0]['conn'] ?? '') === 'sqlite' && ($lq[0]['driver'] ?? '') === 'sqlite', json_encode($lq[0] ?? null));
check('laravel: cache hit', (events_of($r['frames'], 'cache')[0]['key'] ?? '') === 'settings');
check('laravel: view via composing:* wildcard', (events_of($r['frames'], 'view')[0]['name'] ?? '') === 'welcome');
check('laravel: exception from MessageLogged context', (events_of($r['frames'], 'exception')[0]['class'] ?? '') === 'RuntimeException');
check('laravel: log event', (events_of($r['frames'], 'log')[0]['level'] ?? '') === 'error');
if (PHP_VERSION_ID >= 80200 && in_array('sqlite', $drivers, true)) {
    check('laravel: raw PDO not double-captured', count(array_filter($queries, static fn ($q) => $q['sql'] === 'select 1')) === 0);
} else {
    skip('laravel: raw PDO not double-captured', 'needs PHP 8.2+ and the PDO sqlite driver');
}

echo "\nRESULT: " . ($failed === 0 ? 'PASS' : "FAIL ($failed)") . "\n";
exit($failed === 0 ? 0 : 1);
