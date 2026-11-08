<?php
/**
 * devctl dump prepend: sends structured JSON node trees to the dump TCP server.
 *
 * Install by adding to php.ini:
 *   auto_prepend_file = /path/to/prepend.php
 *
 * dump(), dd(), php_dd() and every Symfony VarDumper::dump() call send a
 * base64-encoded JSON payload to the TCP server (default 127.0.0.1:9912).
 */

function _devctl_dump_to_page(): bool
{
    return false; // devctl:dump_to_page
}

if (!function_exists('dd')) {
    function dd(...$vars): never
    {
        _devctl_dump($vars);
        exit(1);
    }
}

if (!function_exists('dump')) {
    function dump(...$vars)
    {
        return _devctl_dump($vars);
    }
}

if (!function_exists('php_dd')) {
    function php_dd(...$vars)
    {
        return _devctl_dump($vars);
    }
}

_devctl_hook_var_dumper();

/**
 * Makes VarDumper::dump() (Laravel Http::dump(), Saloon, direct calls) reach
 * devctl. The class is loaded here from the app's vendor dir because no PHP
 * hook runs after Composer loads a class, and Composer's autoloader always
 * runs before any autoloader a prepend file registers. Setting
 * VAR_DUMPER_FORMAT afterwards makes VarDumper::setHandler() a no-op, so
 * Laravel's FoundationServiceProvider cannot replace the handler.
 * A user who sets VAR_DUMPER_FORMAT keeps full control of VarDumper.
 */
function _devctl_hook_var_dumper(): void
{
    if (isset($_SERVER['VAR_DUMPER_FORMAT'])) {
        return;
    }
    $class = 'Symfony\Component\VarDumper\VarDumper';
    if (!class_exists($class, false)) {
        $file = _devctl_find_var_dumper();
        if ($file === null) {
            return;
        }
        try {
            require_once $file;
        } catch (\Throwable $e) {
            return;
        }
    }
    $class::setHandler(function ($var, $label = null) {
        _devctl_dump([$var]);
    });
    $_SERVER['VAR_DUMPER_FORMAT'] = 'devctl';
}

function _devctl_find_var_dumper(): ?string
{
    $script = $_SERVER['SCRIPT_FILENAME'] ?? '';
    $real   = $script !== '' ? realpath($script) : false;
    $dir    = $real !== false ? dirname($real) : getcwd();
    while (is_string($dir)) {
        $file = $dir . '/vendor/symfony/var-dumper/VarDumper.php';
        if (is_file($file)) {
            return $file;
        }
        $parent = dirname($dir);
        if ($parent === $dir) {
            return null;
        }
        $dir = $parent;
    }
    return null;
}

/**
 * Sends $vars to devctl, prints them too when "also dump to the page" is on,
 * and returns what Symfony's dump() returns.
 */
function _devctl_dump(array $vars)
{
    _devctl_send_dumps($vars);
    if (_devctl_dump_to_page()) {
        foreach ($vars as $var) {
            _devctl_page_dump($var);
        }
    }
    if (!$vars) {
        return null;
    }
    return count($vars) > 1 ? $vars : reset($vars);
}

function _devctl_page_dump($var): void
{
    $cli = in_array(PHP_SAPI, ['cli', 'phpdbg'], true);
    if (class_exists('Symfony\Component\VarDumper\Cloner\VarCloner')) {
        $dumper = $cli
            ? new \Symfony\Component\VarDumper\Dumper\CliDumper()
            : new \Symfony\Component\VarDumper\Dumper\HtmlDumper();
        $dumper->dump((new \Symfony\Component\VarDumper\Cloner\VarCloner())->cloneVar($var));
        return;
    }
    ob_start();
    var_dump($var);
    $out = (string)ob_get_clean();
    echo $cli ? $out : '<pre>' . htmlspecialchars($out) . '</pre>';
}

function _devctl_send_dumps(array $vars): void
{
    $host   = $_SERVER['HTTP_HOST']     ?? '';
    $file   = $_SERVER['SCRIPT_FILENAME'] ?? '';
    $trace  = debug_backtrace(DEBUG_BACKTRACE_IGNORE_ARGS);

    // The source is the first frame outside this file and outside vendor/, so
    // Laravel's ->dump() / ->dd() helpers and VarDumper::dump() point at the
    // user's call. For Blade this is the compiled view cache file
    // (storage/framework/views/...), resolved further below.
    $frame = [];
    $thisFile = __FILE__;
    foreach ($trace as $f) {
        $path = $f['file'] ?? '';
        if ($path === '' || $path === $thisFile) {
            continue;
        }
        if (empty($frame)) {
            $frame = $f;
        }
        if (strpos($path, '/vendor/') === false) {
            $frame = $f;
            break;
        }
    }
    if (empty($frame)) {
        $frame = $trace[0] ?? [];
    }

    $line   = (int)($frame['line'] ?? 0);
    $source = $frame['file'] ?? $file;

    // If the source is a compiled Blade cache file, try to resolve it back to
    // the original .blade.php. The compiled file often contains a reference to
    // the original path (e.g. from Blade debug extensions or error templates).
    // We scan the first 2 KB of the compiled file for a .blade.php path.
    if (str_contains($source, '/framework/views/') && is_readable($source)) {
        $head = file_get_contents($source, false, null, 0, 2048);
        if ($head !== false && preg_match('#[\w./\-]+\.blade\.php#', $head, $m)) {
            $rel = $m[0];
            // Resolve relative path: walk up from the cache file's directory to
            // find the app root (directory containing a composer.json).
            $dir = dirname($source);
            for ($up = 0; $up < 8; $up++) {
                $dir = dirname($dir);
                if (is_file($dir . '/composer.json')) {
                    $candidate = $dir . '/' . $rel;
                    if (is_file($candidate)) {
                        $source = $candidate;
                    }
                    break;
                }
            }
        }
    }

    $payload = [
        'timestamp' => microtime(true),
        'source'    => [
            'file' => $source,
            'line' => $line,
            'name' => basename($source),
        ],
        'host'  => $host,
        'nodes' => array_map('_devctl_encode_value', array_values($vars)),
    ];

    $json    = json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    $encoded = base64_encode($json) . "\n";

    $sock = @stream_socket_client(
        'tcp://127.0.0.1:9912',
        $errno,
        $errstr,
        1,  // 1 second timeout
        STREAM_CLIENT_CONNECT
    );

    if ($sock === false) {
        return; // silently fail if server not running
    }

    @fwrite($sock, $encoded);
    @fclose($sock);
}

/**
 * Recursively encode a PHP value into a structured node.
 *
 * @param mixed $value
 * @param int   $depth   current recursion depth (prevents infinite loops)
 * @param array $seen    object IDs already serialised (back-references)
 */
function _devctl_encode_value(mixed $value, int $depth = 0, array &$seen = []): array
{
    if ($depth > 10) {
        return ['type' => 'scalar', 'kind' => 'const', 'value' => '…truncated…'];
    }

    if ($value === null) {
        return ['type' => 'scalar', 'kind' => 'null', 'value' => null];
    }

    if (is_bool($value)) {
        return ['type' => 'scalar', 'kind' => 'bool', 'value' => $value];
    }

    if (is_int($value)) {
        return ['type' => 'scalar', 'kind' => 'int', 'value' => $value];
    }

    if (is_float($value)) {
        return ['type' => 'scalar', 'kind' => 'float', 'value' => $value];
    }

    if (is_string($value)) {
        $len     = strlen($value);
        $binary  = !mb_check_encoding($value, 'UTF-8');
        $maxLen  = 10000;
        $trunc   = max(0, $len - $maxLen);
        return [
            'type'      => 'string',
            'value'     => $trunc > 0 ? substr($value, 0, $maxLen) : $value,
            'length'    => $len,
            'binary'    => $binary,
            'truncated' => $trunc,
        ];
    }

    if (is_array($value)) {
        $indexed  = array_keys($value) === range(0, count($value) - 1);
        $maxItems = 100;
        $trunc    = max(0, count($value) - $maxItems);
        $children = [];
        $i = 0;
        foreach ($value as $k => $v) {
            if ($i >= $maxItems) {
                break;
            }
            $children[] = [
                'key'   => _devctl_encode_value($k, $depth + 1, $seen),
                'value' => _devctl_encode_value($v, $depth + 1, $seen),
            ];
            $i++;
        }
        return [
            'type'      => 'array',
            'count'     => count($value),
            'indexed'   => $indexed,
            'truncated' => $trunc,
            'children'  => $children,
        ];
    }

    if (is_object($value)) {
        $id = spl_object_id($value);
        if (isset($seen[$id])) {
            return ['type' => 'ref', 'refId' => $id, 'refCount' => $seen[$id]];
        }
        $seen[$id] = $id;

        $class    = get_class($value);
        $props    = (array)$value;  // cast to get protected/private props
        $maxProps = 50;
        $trunc    = max(0, count($props) - $maxProps);
        $children = [];
        $i = 0;
        foreach ($props as $rawKey => $v) {
            if ($i >= $maxProps) {
                break;
            }
            [$visibility, $name] = _devctl_parse_prop_key($rawKey);
            $children[] = [
                'visibility' => $visibility,
                'name'       => $name,
                'value'      => _devctl_encode_value($v, $depth + 1, $seen),
            ];
            $i++;
        }
        return [
            'type'      => 'object',
            'class'     => $class,
            'truncated' => $trunc,
            'children'  => $children,
        ];
    }

    if (is_resource($value)) {
        return [
            'type'         => 'resource',
            'resourceType' => get_resource_type($value),
            'children'     => [],
        ];
    }

    // Fallback
    return ['type' => 'scalar', 'kind' => 'const', 'value' => (string)$value];
}

/**
 * Parse a cast-to-array property key into [visibility, name].
 * PHP encodes visibility as:
 *   public:    "propName"
 *   protected: "\0*\0propName"
 *   private:   "\0ClassName\0propName"
 */
function _devctl_parse_prop_key(string $key): array
{
    if ($key[0] !== "\0") {
        return ['public', $key];
    }
    $parts = explode("\0", $key, 3);
    if (count($parts) === 3) {
        $class = $parts[1];
        $name  = $parts[2];
        if ($class === '*') {
            return ['protected', $name];
        }
        return ['private', $name];
    }
    return ['public', $key];
}
