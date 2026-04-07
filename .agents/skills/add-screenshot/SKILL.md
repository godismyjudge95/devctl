---
name: add-screenshot
description: How to add a new dashboard screenshot for a new or existing feature — wiring up scripts/screenshots.js, seeding data into the demo container via scripts/demo.sh, and adding the image reference to the README.
license: MIT
compatibility: opencode
metadata:
  concerns: screenshots, demo, readme, documentation
---

## Overview

Screenshots live in `docs/` and are produced by `scripts/screenshots.js` running against the `devctl-demo` Incus container. The container is created and seeded by `scripts/demo.sh`. The single command to rebuild everything from scratch is:

```sh
make demo
```

To re-run only the screenshot pass (container already running, you just changed `screenshots.js`):

```sh
bash scripts/demo.sh --screenshots-only
```

---

## How the two files relate

| File | Role |
|---|---|
| `scripts/demo.sh` | Creates the container, installs services, seeds all data, sets up port proxies, then calls `screenshots.js` |
| `scripts/screenshots.js` | Puppeteer script — navigates to every page, optionally interacts, and saves `.png` files to `docs/` |

**`screenshots.js` never touches the container directly.** It only calls `http://127.0.0.1:4001` (the host-side port proxy). All data must already be in the container before screenshots run.

---

## Port mapping

The demo container exposes two ports on the host:

| Host port | Container port | Purpose |
|---|---|---|
| `4001` | `4000` | devctl dashboard — all API calls and the Vue SPA |
| `8161` | `8161` | WhoDB — required because `WhoDBView.vue` hardcodes `http://127.0.0.1:8161` in the iframe src |

`BASE_URL=http://127.0.0.1:4001` is passed to `screenshots.js` by `demo.sh`. Standalone use defaults to `http://127.0.0.1:4000`.

---

## Container constants (demo.sh)

```
CONTAINER   = devctl-demo
SERVER_ROOT = /home/testuser/ddev/sites/server
SITES_ROOT  = /home/testuser/ddev/sites
SITE_USER   = testuser
PHP version = 8.3
SPX data    = /home/testuser/ddev/sites/server/php/8.3/spx-data/
```

---

## Adding a screenshot: the two-part change

Every new screenshot requires changes in **both** files plus the README:

1. **`scripts/screenshots.js`** — add an entry to the `PAGES` array
2. **`scripts/demo.sh`** — add seed data for the new feature inside the Python `SEED_SCRIPT` heredoc (or as a separate `incus exec` call for non-Python work)
3. **`README.md`** — add an `![alt](docs/screenshot-name.png)` reference under the relevant section

---

## Part 1 — Adding an entry to screenshots.js

The `PAGES` array drives both the desktop and mobile passes. Each entry:

```js
{
  route:      "/your-route",           // Vue router path
  desktop:    "screenshot-name.png",   // saved to docs/
  mobile:     "screenshot-mobile-name.png",
  extraWait:  0,                       // optional: extra ms after navigate (iframe-heavy pages)
  async before(page) { /* optional */ },  // runs after navigate+settle, before snap
}
```

### The `before()` hook — when and how to use it

Use `before()` whenever the page needs interaction to show meaningful content:

- **Click an item to open a detail panel** (mail, SPX, logs all use this)
- **Wait for a specific element** before snapping
- **Open a dialog or modal** that you want captured

Available helpers (defined at the top of `screenshots.js`):

```js
// Navigate and wait for networkidle2
await nav(page, "/route")

// Click the first element matching selector, wait afterMs
await clickFirst(page, ".some-selector", 800)

// Raw sleep
await sleep(500)

// Save screenshot
await snap(page, "filename.png")
```

### Selector patterns for common UI shapes

| UI pattern | Selector to use | Notes |
|---|---|---|
| Clickable list rows (mail, SPX) | `.cursor-pointer` | Tailwind class applied to all interactive list items |
| Sidebar log file buttons | `aside button` | Log file items are `<button>` inside `<aside>` |
| shadcn Dialog trigger | `button[aria-haspopup="dialog"]` | Standard shadcn pattern |
| Tab trigger | `button[role="tab"]` | Switch tabs before snapping |
| First table row | `tbody tr:first-child` | For any shadcn Table |
| Any button by text | `button ::-p-text(Save)` | Puppeteer text selector |

### Example — clicking to open a detail panel

```js
{
  route:   "/my-feature",
  desktop: "screenshot-my-feature.png",
  mobile:  "screenshot-mobile-my-feature.png",
  async before(page) {
    // Wait for list to populate, then click first item
    await page.waitForFunction(
      () => document.querySelectorAll(".cursor-pointer").length > 0,
      { timeout: 5000 }
    ).catch(() => {});
    await clickFirst(page, ".cursor-pointer", 800);
  },
},
```

### Example — opening a modal/dialog for the screenshot

```js
{
  route:   "/services",
  desktop: "screenshot-services-dialog.png",
  mobile:  "screenshot-mobile-services-dialog.png",
  async before(page) {
    // Click the settings gear on the first service row
    await page.waitForSelector('[aria-label="Settings"]', { timeout: 5000 }).catch(() => {});
    await clickFirst(page, '[aria-label="Settings"]', 600);
    // Wait for the dialog to appear
    await page.waitForSelector('[role="dialog"]', { timeout: 3000 }).catch(() => {});
    await sleep(400);
  },
},
```

### Example — switching to a non-default tab

```js
{
  route:   "/spx",
  desktop: "screenshot-spx-flamegraph.png",
  mobile:  "screenshot-mobile-spx-flamegraph.png",
  async before(page) {
    await page.waitForSelector(".cursor-pointer", { timeout: 5000 }).catch(() => {});
    await clickFirst(page, ".cursor-pointer", 800);
    // Click the Flamegraph tab
    await page.waitForSelector('button[role="tab"]', { timeout: 3000 }).catch(() => {});
    const tabs = await page.$$('button[role="tab"]');
    for (const tab of tabs) {
      const text = await tab.evaluate(el => el.textContent);
      if (text?.includes("Flamegraph")) { await tab.click(); await sleep(600); break; }
    }
  },
},
```

---

## Part 2 — Seeding data in demo.sh

All seed code runs inside the Python `SEED_SCRIPT` heredoc near the bottom of `demo.sh`. The heredoc is executed with:

```bash
incus exec "$CONTAINER" -- python3 - <<'SEED_SCRIPT'
...
SEED_SCRIPT
```

`incus exec` runs inside the container, so `127.0.0.1` refers to the container's loopback, not the host.

### Seeding patterns by data type

#### TCP Dumps (port 9912)

Wire format: one base64-encoded JSON line per dump, terminated with `\n`.

```python
import base64, json, socket, time

dump = {
    "timestamp": time.time(),
    "source": {
        "file": "/home/testuser/ddev/sites/laravel.test/app/Http/Controllers/MyController.php",
        "line": 42,
        "name": "MyController.php",
    },
    "host": "laravel.test",   # must match a registered site domain for site_domain to populate
    "nodes": [
        # Scalar types
        {"type": "scalar", "kind": "int",   "value": 42},
        {"type": "scalar", "kind": "float", "value": 3.14},
        {"type": "scalar", "kind": "bool",  "value": True},
        {"type": "scalar", "kind": "null",  "value": None},
        # String
        {"type": "string", "value": "hello", "length": 5, "binary": False, "truncated": 0},
        # Array (associative)
        {
            "type": "array", "count": 1, "indexed": False, "truncated": 0,
            "children": [
                {
                    "key":   {"type": "string", "value": "key", "length": 3, "binary": False, "truncated": 0},
                    "value": {"type": "string", "value": "val", "length": 3, "binary": False, "truncated": 0},
                }
            ],
        },
        # Object
        {
            "type": "object", "class": "App\\Models\\User", "truncated": 0,
            "children": [
                {
                    "visibility": "public",
                    "name": "name",
                    "value": {"type": "string", "value": "Alice", "length": 5, "binary": False, "truncated": 0},
                }
            ],
        },
    ],
}

s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.settimeout(5)
s.connect(("127.0.0.1", 9912))
s.sendall(base64.b64encode(json.dumps(dump).encode()) + b"\n")
s.close()
```

#### Mail (Mailpit API via devctl proxy)

```python
import json, urllib.request

msg = {
    "From": {"Email": "noreply@laravel.test", "Name": "My App"},
    "To":   [{"Email": "dev@example.com",     "Name": "Developer"}],
    "Subject": "Subject here",
    "Text":    "Plain text body.",
    "HTML":    "<p>HTML body.</p>",   # optional
}
data = json.dumps(msg).encode()
req = urllib.request.Request(
    "http://127.0.0.1:4000/api/mail/api/v1/send", data=data,
    headers={"Content-Type": "application/json"}, method="POST",
)
with urllib.request.urlopen(req, timeout=10) as resp:
    print(resp.status)
```

#### SPX Profiles (files on disk)

SPX profiles are plain files in `{SERVER_ROOT}/php/{version}/spx-data/`. Each profile needs two files:

- `{key}.json` — metadata (read by `loadProfileMeta`)
- `{key}.txt.gz` — gzipped call trace (read by `parseCallTrace`)

```python
import gzip, io, json, os

SPX_DIR = "/home/testuser/ddev/sites/server/php/8.3/spx-data"
os.makedirs(SPX_DIR, exist_ok=True)

KEY = "spx_my_feature_20240101_120000_abcd1234"

# Metadata (.json)
meta = {
    "http_host":             "laravel.test",   # strip port if present; used as domain
    "http_method":           "GET",
    "http_request_uri":      "/dashboard",
    "wall_time_ms":          67.4,
    "peak_memory_usage":     4194304,           # bytes
    "called_function_count": 215,
    "exec_ts":               1704067200,        # unix timestamp
}
with open(os.path.join(SPX_DIR, KEY + ".json"), "w") as f:
    json.dump(meta, f)

# Call trace (.txt.gz)
# Format:
#   [events]
#   {func_idx} {is_enter:1=enter,0=exit} {wall_time_microseconds}
#   (blank line)
#   [functions]
#   function_name_0
#   function_name_1
#   ...
functions = [
    "{main}",
    "My\\Namespace\\MyClass::myMethod",
    "Illuminate\\Database\\Query\\Builder::get",
]
events = [
    "0 1 0",
    "1 1 500",
    "2 1 2000",  "2 0 30000",
    "1 0 55000",
    "0 0 67400",  # last exit wall_time_us should equal wall_time_ms * 1000
]
content = "[events]\n" + "\n".join(events) + "\n\n[functions]\n" + "\n".join(functions) + "\n"

buf = io.BytesIO()
with gzip.GzipFile(fileobj=buf, mode="wb", mtime=0) as gz:
    gz.write(content.encode())
with open(os.path.join(SPX_DIR, KEY + ".txt.gz"), "wb") as f:
    f.write(buf.getvalue())

os.system(f"chown -R testuser:testuser {SPX_DIR}")
```

**Call trace rules:**
- Function indices are 0-based, matching the `[functions]` list order
- `is_enter=1` → function call; `is_enter=0` → function return
- Wall times are in **microseconds**, monotonically increasing
- The last exit event's wall time × 0.001 should equal `wall_time_ms` in the JSON
- Every enter must have a matching exit at the same or lower depth
- `parseCallTrace` in `api/spx.go` caps flamegraph events at 5000; flat profile has no cap

#### MaxIO / S3 objects (via devctl proxy)

```python
import json, urllib.request

def s3_put(path, body=b"", content_type="application/octet-stream"):
    req = urllib.request.Request(
        f"http://127.0.0.1:4000/api/maxio/s3{path}", data=body,
        headers={"Content-Type": content_type, "Content-Length": str(len(body))},
        method="PUT",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code

# Create bucket
s3_put("/my-bucket")

# Upload objects
s3_put("/my-bucket/data.json", json.dumps({"key": "value"}).encode(), "application/json")
s3_put("/my-bucket/notes.txt", b"some content", "text/plain")
```

The devctl proxy at `/api/maxio/s3/` reads credentials from `{serverRoot}/maxio/config.env` and signs requests with AWS Signature V4 before forwarding to MinIO at port 9000. No auth headers needed when calling through the proxy.

#### Sites (via devctl REST API)

```python
import json, urllib.request

def create_site(domain, root_path):
    data = json.dumps({"domain": domain, "root_path": root_path}).encode()
    req = urllib.request.Request(
        "http://127.0.0.1:4000/api/sites", data=data,
        headers={"Content-Type": "application/json"}, method="POST",
    )
    with urllib.request.urlopen(req, timeout=10) as r:
        return json.loads(r.read())
```

Framework is auto-detected from files on disk. Marker files needed in the site root:

| Framework | Marker |
|---|---|
| Laravel | `artisan` file |
| Statamic | `vendor/statamic/` directory |
| WordPress | `wp-config.php` file |

#### Services (install via SSE API, wait for running)

If a new feature requires a service that isn't installed by default in the demo, add it to the sequential install block in `demo.sh`:

```bash
install_service "my-service"  "My Service Label"  300   # 300s timeout
wait_running    "my-service"  "My Service Label"  60
```

`install_service` streams the SSE install endpoint and checks for `event: done`. `wait_running` polls `GET /api/services` until `.status == "running"`.

---

## Part 3 — Non-Python setup in demo.sh

For operations that don't fit in the Python SEED_SCRIPT (e.g. file system prep, running CLI tools), use `incus exec` directly before the SEED_SCRIPT block:

```bash
# Create a directory the new feature needs
incus exec "$CONTAINER" -- bash -c "
  mkdir -p '${SERVER_ROOT}/my-feature/data'
  chown -R testuser:testuser '${SERVER_ROOT}/my-feature'
"

# Run a CLI tool inside the container as testuser
incus exec "$CONTAINER" -- su -c "some-command --flag" testuser
```

---

## Part 4 — Adding the image to README.md

Place the image reference directly under the section heading or prose that describes the feature. Use a descriptive alt text. Keep the filename stable — these filenames never change once set:

```markdown
![Brief description of what the screenshot shows](docs/screenshot-name.png)
```

For mobile screenshots, add them to the `<p align="center">` gallery in the **Contributing → Screenshots** section:

```html
<img src="docs/screenshot-mobile-name.png" width="200" alt="Feature on mobile">
```

---

## Checklist

- [ ] Entry added to `PAGES` array in `scripts/screenshots.js` with `desktop`, `mobile`, and optional `before()` / `extraWait`
- [ ] Any required service installed in the `install_service` / `wait_running` block in `demo.sh`
- [ ] Seed data added to the Python `SEED_SCRIPT` heredoc in `demo.sh` (or as `incus exec` calls before it)
- [ ] `README.md` updated with `![alt](docs/screenshot-name.png)` in the correct section
- [ ] Mobile image added to the gallery in Contributing → Screenshots
- [ ] `make demo` runs cleanly end-to-end and the new `.png` files appear in `docs/`
- [ ] Verify screenshots look correct — real data visible, detail panels open where expected
