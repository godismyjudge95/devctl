<?php
/**
 * devctl request inspector agent. prepend.php requires it on PHP 8.1+, and
 * devctl deletes it when the inspector_enabled setting is off.
 *
 * Hooks come from the opentelemetry extension that the devctl 8.1+ static
 * builds include (OpenTelemetry\Instrumentation\hook, Zend observer).
 * Wire format: one JSON object per line on the dump TCP port. See
 * docs/plans/request-inspector-spike.md.
 */

namespace Devctl\Inspector {

    use function OpenTelemetry\Instrumentation\hook;

    final class Agent
    {
        /** CLI entrypoints the agent runs in. true = record the process as a "command" unit. */
        private const CLI_ENTRYPOINTS = [
            'artisan' => true,
            'swoole-server' => false,
            'roadrunner-worker' => false,
        ];

        /** Long-running artisan commands. Only their jobs or Octane requests become units. */
        private const DAEMON_COMMANDS = [
            'queue:work', 'queue:listen', 'horizon', 'horizon:work', 'schedule:work',
            'octane:start', 'octane:swoole', 'octane:roadrunner', 'octane:frankenphp',
            'reverb:start', 'pail',
        ];

        private const MAX_EVENTS = 5000;
        private const BATCH = 200;
        private const MAX_SQL = 10000;
        private const MAX_STRING = 1024;
        private const MAX_BINDINGS = 50;

        /** Current unit id. null means "not recording". */
        public static ?string $rid = null;

        private static string $addr = 'tcp://127.0.0.1:9912';
        private static array $meta = [];
        private static array $events = [];
        private static int $seq = 0;
        private static int $count = 0;
        private static int $dropped = 0;
        private static int $t0 = 0;
        private static bool $laravel = false;
        private static bool $internalHooks = false;
        /** @var int[] hrtime stamps pushed by pre hooks, popped by post hooks */
        private static array $timers = [];
        private static ?\WeakMap $bound = null;
        /** @var array<string, string> compiled Blade file => .blade.php source */
        private static array $blade = [];

        public static function boot(): void
        {
            self::$addr = getenv('DEVCTL_INSPECTOR_ADDR')
                ?: (function_exists('_devctl_dump_addr') ? _devctl_dump_addr() : self::$addr);

            if (PHP_SAPI === 'cli' || PHP_SAPI === 'phpdbg') {
                $entry = basename((string) ($_SERVER['argv'][0] ?? ''));
                if (!array_key_exists($entry, self::CLI_ENTRYPOINTS)) {
                    return;
                }
                if (self::CLI_ENTRYPOINTS[$entry]) {
                    self::begin('command', ['command' => implode(' ', array_slice($_SERVER['argv'], 1))]);
                }
            } else {
                $https = ($_SERVER['HTTPS'] ?? '') !== '' && $_SERVER['HTTPS'] !== 'off';
                $host = $_SERVER['HTTP_HOST'] ?? '';
                self::begin('http', [
                    'method' => $_SERVER['REQUEST_METHOD'] ?? '',
                    'url' => ($https ? 'https://' : 'http://') . $host . ($_SERVER['REQUEST_URI'] ?? ''),
                    'host' => $host,
                ]);
                if (!headers_sent()) {
                    header('X-Devctl-Request-Id: ' . self::$rid);
                }
            }

            hook('Illuminate\Foundation\Application', '__construct', null, self::guard(static function ($app) {
                self::attachLaravel($app);
            }));

            // Internal functions are observable only on PHP 8.2+.
            if (PHP_VERSION_ID >= 80200) {
                self::$internalHooks = true;
                self::hookInternals();
            }

            // Registering from inside a shutdown function puts ours after the app's own.
            register_shutdown_function(static function () {
                register_shutdown_function([self::class, 'shutdown']);
            });
        }

        public static function shutdown(): void
        {
            if (self::$rid === null) {
                return;
            }
            $err = error_get_last();
            if ($err !== null && in_array($err['type'], [E_ERROR, E_PARSE, E_CORE_ERROR, E_COMPILE_ERROR, E_USER_ERROR], true)) {
                self::record('exception', [
                    'class' => 'FatalError',
                    'message' => self::clip($err['message'], 4000),
                    'file' => $err['file'],
                    'line' => $err['line'],
                ], false);
            }
            $end = [];
            if (PHP_SAPI !== 'cli' && PHP_SAPI !== 'phpdbg') {
                $end['status'] = http_response_code() ?: 200;
            }
            self::end($end);
        }

        private static function begin(string $kind, array $meta, ?string $parent = null): void
        {
            if (self::$rid !== null) {
                self::end();
            }
            self::$rid = bin2hex(random_bytes(8));
            self::$t0 = hrtime(true);
            self::$seq = 0;
            self::$count = 0;
            self::$dropped = 0;
            self::$events = [];
            self::$meta = $meta + [
                'kind' => $kind,
                'parent' => $parent,
                'pid' => getmypid(),
                'php' => PHP_VERSION,
                'sapi' => PHP_SAPI,
                'started_at' => microtime(true),
            ];
        }

        private static function end(array $meta = []): void
        {
            if (self::$rid === null) {
                return;
            }
            self::$meta = array_merge(self::$meta, $meta, [
                'duration_ms' => self::sinceStart(),
                'memory_peak' => memory_get_peak_usage(true),
                'dropped' => self::$dropped,
            ]);
            self::flush(true);
            self::$rid = null;
        }

        private static function discard(): void
        {
            self::$rid = null;
            self::$events = [];
        }

        private static function record(string $kind, array $fields, bool $withCaller = true): void
        {
            if (self::$rid === null) {
                return;
            }
            if (self::$count >= self::MAX_EVENTS) {
                self::$dropped++;
                return;
            }
            $fields = ['kind' => $kind, 't' => self::sinceStart()] + $fields;
            if ($withCaller && ($caller = self::caller()) !== null) {
                $fields['caller'] = $caller;
            }
            self::$events[] = $fields;
            self::$count++;
            if (count(self::$events) >= self::BATCH) {
                self::flush(false);
            }
        }

        private static function flush(bool $final): void
        {
            $frame = [
                'v' => 1,
                'type' => 'unit',
                'rid' => self::$rid,
                'seq' => self::$seq++,
                'final' => $final,
                'meta' => self::$meta,
                'events' => self::$events,
            ];
            self::$events = [];
            $json = json_encode($frame, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE | JSON_PARTIAL_OUTPUT_ON_ERROR);
            if ($json === false) {
                return;
            }
            $sock = @stream_socket_client(self::$addr, $errno, $errstr, 0.2);
            if ($sock === false) {
                return;
            }
            $buf = $json . "\n";
            while ($buf !== '') {
                $n = @fwrite($sock, $buf);
                if ($n === false || $n === 0) {
                    break;
                }
                $buf = (string) substr($buf, $n);
            }
            @fclose($sock);
        }

        /** First frame outside vendor/ and devctl's own files; first non-devctl frame if all are vendor. */
        private static function caller(): ?array
        {
            $own = __DIR__ . DIRECTORY_SEPARATOR;
            $fallback = null;
            foreach (debug_backtrace(DEBUG_BACKTRACE_IGNORE_ARGS) as $f) {
                $file = $f['file'] ?? null;
                if ($file === null || strncmp($file, $own, strlen($own)) === 0) {
                    continue;
                }
                if (strpos($file, '/framework/views/') !== false && function_exists('_devctl_blade_source')) {
                    $file = self::$blade[$file] ??= _devctl_blade_source($file);
                }
                $frame = ['file' => $file, 'line' => $f['line'] ?? 0];
                if (strpos($file, DIRECTORY_SEPARATOR . 'vendor' . DIRECTORY_SEPARATOR) === false) {
                    return $frame;
                }
                $fallback ??= $frame;
            }
            return $fallback;
        }

        private static function attachLaravel($app): void
        {
            if (self::$laravel) {
                return;
            }
            self::$laravel = true;

            $attach = self::guard(static function ($events) {
                foreach (self::laravelListeners() as $event => $listener) {
                    $events->listen($event, self::guard($listener));
                }
            });
            if ($app->resolved('events')) {
                $attach($app->make('events'));
            } else {
                $app->afterResolving('events', $attach);
            }

            if (method_exists('Illuminate\Queue\Queue', 'createPayloadUsing')) {
                \Illuminate\Queue\Queue::createPayloadUsing(static function () {
                    return self::$rid === null ? [] : ['devctl_rid' => self::$rid];
                });
            }
        }

        /** @return array<string, \Closure> Laravel event name => listener */
        private static function laravelListeners(): array
        {
            $listeners = [
                'Illuminate\Database\Events\QueryExecuted' => static function ($e) {
                    self::record('query', [
                        'sql' => self::clip($e->sql, self::MAX_SQL),
                        'bindings' => self::bindings($e->bindings),
                        'ms' => $e->time,
                        'conn' => $e->connectionName,
                        'driver' => method_exists($e->connection, 'getDriverName') ? $e->connection->getDriverName() : null,
                    ]);
                },
                'Illuminate\Log\Events\MessageLogged' => static function ($e) {
                    $ex = $e->context['exception'] ?? null;
                    if ($ex instanceof \Throwable) {
                        self::record('exception', self::exception($ex), false);
                    }
                    self::record('log', ['level' => $e->level, 'message' => self::clip((string) $e->message, 4000)]);
                },
                'Illuminate\Cache\Events\CacheHit' => static fn ($e) => self::cache('hit', $e),
                'Illuminate\Cache\Events\CacheMissed' => static fn ($e) => self::cache('miss', $e),
                'Illuminate\Cache\Events\KeyWritten' => static fn ($e) => self::cache('write', $e),
                'Illuminate\Cache\Events\KeyForgotten' => static fn ($e) => self::cache('forget', $e),
                'composing:*' => static function ($name, array $data) {
                    $view = $data[0] ?? null;
                    if (is_object($view) && method_exists($view, 'name')) {
                        self::record('view', ['name' => $view->name(), 'path' => method_exists($view, 'getPath') ? $view->getPath() : null]);
                    }
                },
                'Illuminate\Queue\Events\JobQueued' => static function ($e) {
                    self::record('job', [
                        'phase' => 'queued',
                        'name' => is_object($e->job) ? get_class($e->job) : (string) $e->job,
                        'connection' => $e->connectionName,
                        'id' => $e->id ?? null,
                    ]);
                },
                'Illuminate\Queue\Events\JobProcessing' => static function ($e) {
                    if ($e->connectionName === 'sync') {
                        self::record('job', ['phase' => 'processing', 'name' => $e->job->resolveName(), 'connection' => 'sync']);
                        return;
                    }
                    $payload = $e->job->payload();
                    self::begin('job', [
                        'job' => $e->job->resolveName(),
                        'queue' => $e->job->getQueue(),
                        'connection' => $e->connectionName,
                        'job_id' => $e->job->getJobId(),
                        'attempt' => $e->job->attempts(),
                    ], $payload['devctl_rid'] ?? null);
                },
                'Illuminate\Queue\Events\JobProcessed' => static function ($e) {
                    if ($e->connectionName === 'sync') {
                        self::record('job', ['phase' => 'processed', 'name' => $e->job->resolveName(), 'connection' => 'sync']);
                        return;
                    }
                    self::end(['job_status' => 'processed']);
                },
                'Illuminate\Queue\Events\JobExceptionOccurred' => static function ($e) {
                    self::record('exception', self::exception($e->exception), false);
                    if ($e->connectionName !== 'sync') {
                        self::end(['job_status' => 'failed']);
                    }
                },
                'Illuminate\Foundation\Http\Events\RequestHandled' => static function ($e) {
                    $route = $e->request->route();
                    if (is_object($route)) {
                        self::$meta['route'] = $route->getName() ?? $route->uri();
                        self::$meta['action'] = $route->getActionName();
                    }
                },
                'Illuminate\Console\Events\CommandStarting' => static function ($e) {
                    if (self::$rid === null || isset(self::$meta['artisan'])) {
                        return;
                    }
                    if (in_array($e->command, self::DAEMON_COMMANDS, true)) {
                        self::discard();
                        return;
                    }
                    self::$meta['artisan'] = $e->command;
                },
                'Laravel\Octane\Events\RequestReceived' => static function ($e) {
                    self::begin('http', [
                        'method' => $e->request->getMethod(),
                        'url' => $e->request->getUri(),
                        'host' => $e->request->getHost(),
                    ]);
                },
                'Laravel\Octane\Events\RequestTerminated' => static function ($e) {
                    self::end(['status' => $e->response->getStatusCode()]);
                },
            ];

            // On 8.2+ the curl_exec hook already sees every outgoing request, Guzzle included.
            if (!self::$internalHooks) {
                $listeners['Illuminate\Http\Client\Events\ResponseReceived'] = static function ($e) {
                    $stats = method_exists($e->response, 'handlerStats') ? $e->response->handlerStats() : [];
                    self::record('http', [
                        'method' => $e->request->method(),
                        'url' => $e->request->url(),
                        'status' => $e->response->status(),
                        'ms' => isset($stats['total_time']) ? round($stats['total_time'] * 1000, 3) : null,
                    ]);
                };
                $listeners['Illuminate\Http\Client\Events\ConnectionFailed'] = static function ($e) {
                    self::record('http', ['method' => $e->request->method(), 'url' => $e->request->url(), 'error' => 'connection failed']);
                };
            }

            return $listeners;
        }

        private static function hookInternals(): void
        {
            self::$bound = new \WeakMap();
            $pre = static function () {
                self::$timers[] = hrtime(true);
            };

            /** [class, method, fn(object|null, params) => [sql, bindings, conn]] */
            $sqlSources = [
                ['PDO', 'exec', static fn ($o, array $p) => [$p[0] ?? '', [], 'pdo']],
                ['PDO', 'query', static fn ($o, array $p) => [$p[0] ?? '', [], 'pdo']],
                ['PDOStatement', 'execute', static fn ($o, array $p) => [$o->queryString, $p[0] ?? (self::$bound[$o] ?? []), 'pdo']],
                [null, 'mysqli_query', static fn ($o, array $p) => [$p[1] ?? '', [], 'mysqli']],
                ['mysqli', 'query', static fn ($o, array $p) => [$p[0] ?? '', [], 'mysqli']],
            ];
            foreach ($sqlSources as [$class, $function, $extract]) {
                hook($class, $function, $pre, self::guard(static function ($o, array $params, $ret, $ex) use ($extract) {
                    $ms = self::stop();
                    if (self::$rid === null || self::$laravel) {
                        return;
                    }
                    [$sql, $bindings, $conn] = $extract($o, $params);
                    $fields = [
                        'sql' => self::clip((string) $sql, self::MAX_SQL),
                        'bindings' => self::bindings((array) $bindings),
                        'ms' => $ms,
                        'conn' => $conn,
                    ];
                    if ($ex instanceof \Throwable) {
                        $fields['error'] = self::clip($ex->getMessage(), 2000);
                    }
                    self::record('query', $fields);
                }));
            }

            hook('PDOStatement', 'bindValue', null, self::guard(static function ($stmt, array $params) {
                $bound = self::$bound[$stmt] ?? [];
                $bound[$params[0]] = $params[1] ?? null;
                self::$bound[$stmt] = $bound;
            }));

            hook(null, 'curl_exec', $pre, self::guard(static function ($o, array $params, $ret) {
                $ms = self::stop();
                $handle = $params[0] ?? null;
                if (self::$rid === null || $handle === null) {
                    return;
                }
                $fields = [
                    'method' => defined('CURLINFO_EFFECTIVE_METHOD') ? curl_getinfo($handle, CURLINFO_EFFECTIVE_METHOD) : null,
                    'url' => curl_getinfo($handle, CURLINFO_EFFECTIVE_URL),
                    'status' => curl_getinfo($handle, CURLINFO_RESPONSE_CODE),
                    'ms' => $ms,
                ];
                if ($ret === false) {
                    $fields['error'] = curl_error($handle);
                }
                self::record('http', $fields);
            }));
        }

        private static function cache(string $op, $e): void
        {
            self::record('cache', ['op' => $op, 'key' => (string) $e->key, 'store' => $e->storeName ?? null]);
        }

        private static function exception(\Throwable $ex): array
        {
            return [
                'class' => get_class($ex),
                'message' => self::clip($ex->getMessage(), 4000),
                'file' => $ex->getFile(),
                'line' => $ex->getLine(),
                'trace' => array_map(
                    static fn ($f) => ['file' => $f['file'] ?? null, 'line' => $f['line'] ?? null, 'fn' => ($f['class'] ?? '') . ($f['type'] ?? '') . ($f['function'] ?? '')],
                    array_slice($ex->getTrace(), 0, 20)
                ),
            ];
        }

        private static function bindings(array $bindings): array
        {
            $out = [];
            $n = 0;
            foreach ($bindings as $key => $value) {
                if (++$n > self::MAX_BINDINGS) {
                    $out['…'] = count($bindings) - self::MAX_BINDINGS;
                    break;
                }
                $out[$key] = self::scalar($value);
            }
            return $out;
        }

        private static function scalar($v)
        {
            if ($v === null || is_bool($v) || is_int($v) || is_float($v)) {
                return $v;
            }
            if ($v instanceof \DateTimeInterface) {
                return $v->format('Y-m-d H:i:s.u');
            }
            if (is_object($v)) {
                return method_exists($v, '__toString') ? self::scalar((string) $v) : '<' . get_class($v) . '>';
            }
            if (!is_string($v)) {
                return '<' . gettype($v) . '>';
            }
            if (preg_match('//u', $v) !== 1) {
                return '<binary ' . strlen($v) . ' bytes>';
            }
            return self::clip($v, self::MAX_STRING);
        }

        private static function clip(string $s, int $max): string
        {
            return strlen($s) > $max ? substr($s, 0, $max) . '…' : $s;
        }

        private static function stop(): ?float
        {
            $t = array_pop(self::$timers);
            return $t === null ? null : round((hrtime(true) - $t) / 1e6, 3);
        }

        private static function sinceStart(): float
        {
            return round((hrtime(true) - self::$t0) / 1e6, 3);
        }

        /**
         * Wraps a hook or listener so it can never break the app. Post hooks must
         * not declare a return type: opentelemetry would then replace the hooked
         * function's return value with ours.
         */
        private static function guard(\Closure $fn): \Closure
        {
            return static function (...$args) use ($fn) {
                try {
                    $fn(...$args);
                } catch (\Throwable $e) {
                }
            };
        }
    }
}

namespace {
    if (PHP_VERSION_ID >= 80100 && function_exists('OpenTelemetry\Instrumentation\hook') && !function_exists('devctl_request_id')) {
        /** The current inspector unit id, for the dump sender to tag dumps with. */
        function devctl_request_id(): ?string
        {
            return \Devctl\Inspector\Agent::$rid;
        }

        \Devctl\Inspector\Agent::boot();
    }
}
