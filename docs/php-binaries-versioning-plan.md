# PHP Binaries Versioning And Update Metadata Plan

## Goals

1. Stop using the mutable `php-binaries-latest` release tag for downloadable PHP assets.
2. Publish PHP binaries under unique tags such as `php-binaries-20260422.1`.
3. Attach a manifest to each PHP-binaries release that records the exact PHP patch version built for each supported minor.
4. Make devctl always discover and install from the newest `php-binaries-*` release.
5. Make the PHP update UI show real patch upgrades such as `8.4.18 -> 8.4.19`.
6. Cover the new release discovery and update behavior with tests that run safely in the Incus test environment.

## Current Problems

1. `php-binaries-latest` reuses the same asset URLs after release replacement.
2. GitHub/CDN caching can serve stale assets even after a new PHP release is published.
3. devctl has no manifest or metadata source for PHP patch versions, so it cannot tell users what PHP patch release is available.
4. PHP is not represented by an installer in `install.NewRegistry`, so it does not participate in the existing `LatestVersion()` update-check pipeline.

## Release Model

### Tags

- devctl app releases remain `vX.Y.Z`
- PHP binary releases become unique tags like `php-binaries-20260422.1`

When both should ship from the same commit, create both tags on that commit. The workflows remain separate.

### Workflow Ownership

- `.github/workflows/release.yml`
  - Runs only for `v*`
  - Builds and uploads the `devctl` binary
- `.github/workflows/build-php.yml`
  - Runs only for `php-binaries*`
  - Builds PHP 8.1 through 8.4 CLI and FPM binaries
  - Produces and uploads a manifest asset

## Manifest Design

### Asset Name

Use a stable manifest filename on every PHP release:

`php-binaries.json`

### Proposed Schema

```json
{
  "release_tag": "php-binaries-20260422.1",
  "built_at": "2026-04-22T21:07:11Z",
  "php_versions": {
    "8.1": "8.1.33",
    "8.2": "8.2.29",
    "8.3": "8.3.22",
    "8.4": "8.4.19"
  },
  "assets": {
    "8.1": {
      "cli": "php-8.1-cli-linux-x86_64",
      "fpm": "php-8.1-fpm-linux-x86_64"
    },
    "8.2": {
      "cli": "php-8.2-cli-linux-x86_64",
      "fpm": "php-8.2-fpm-linux-x86_64"
    },
    "8.3": {
      "cli": "php-8.3-cli-linux-x86_64",
      "fpm": "php-8.3-fpm-linux-x86_64"
    },
    "8.4": {
      "cli": "php-8.4-cli-linux-x86_64",
      "fpm": "php-8.4-fpm-linux-x86_64"
    }
  }
}
```

### Why This Shape

1. `release_tag` gives devctl the exact tag to download assets from.
2. `php_versions` gives the patch version per supported minor.
3. `assets` avoids hardcoding asset naming logic in multiple places.
4. `built_at` helps debugging and makes stale-cache incidents easier to reason about.

## GitHub Workflow Changes

## 1. `build-php.yml`

### Triggering

Keep the current `startsWith(github.ref_name, 'php-binaries')` guard.

### Build output collection

After each matrix job builds the CLI and FPM binaries:

1. Run the built CLI binary with `-v`
2. Parse the full patch version from the first line
3. Write a small per-version metadata file into the job workspace, for example:

```json
{
  "minor": "8.4",
  "patch": "8.4.19",
  "cli": "php-8.4-cli-linux-x86_64",
  "fpm": "php-8.4-fpm-linux-x86_64"
}
```

4. Upload that metadata as an artifact, or keep it in the release-upload step if the workflow is restructured into multiple jobs.

### Manifest assembly

Recommended structure:

1. Matrix build job per PHP minor builds binaries and uploads temporary metadata artifacts.
2. Final manifest job depends on all matrix jobs.
3. Final manifest job downloads metadata artifacts, assembles `php-binaries.json`, and uploads it to the same GitHub release.

This is cleaner than trying to mutate one shared file from four parallel jobs.

### Release assets

Each PHP release should end up with:

- `php-8.1-cli-linux-x86_64`
- `php-8.1-fpm-linux-x86_64`
- `php-8.2-cli-linux-x86_64`
- `php-8.2-fpm-linux-x86_64`
- `php-8.3-cli-linux-x86_64`
- `php-8.3-fpm-linux-x86_64`
- `php-8.4-cli-linux-x86_64`
- `php-8.4-fpm-linux-x86_64`
- `php-binaries.json`

## 2. `release.yml`

Keep the workflow restricted to `v*` tags only so PHP releases never upload a `devctl` asset again.

## Backend Changes

## 1. Add PHP release discovery code

Create a small PHP-specific release metadata layer, likely under `php/`.

Suggested responsibilities:

1. Query GitHub Releases for the repo.
2. Find the newest release whose tag starts with `php-binaries-`.
3. Fetch the `php-binaries.json` asset for that release.
4. Return parsed metadata plus the release tag.

Suggested new file(s):

- `php/releases.go`
- `php/releases_test.go`

Suggested types:

```go
type ReleaseManifest struct {
    ReleaseTag  string                    `json:"release_tag"`
    BuiltAt     string                    `json:"built_at"`
    PHPVersions map[string]string         `json:"php_versions"`
    Assets      map[string]ReleaseAssets  `json:"assets"`
}

type ReleaseAssets struct {
    CLI string `json:"cli"`
    FPM string `json:"fpm"`
}
```

Suggested functions:

```go
func LatestReleaseTag(ctx context.Context) (string, error)
func FetchReleaseManifest(ctx context.Context, tag string) (*ReleaseManifest, error)
func LatestReleaseManifest(ctx context.Context) (*ReleaseManifest, error)
```

## 2. Change PHP installer download resolution

Current behavior:

- `php/installer.go` hardcodes one release base URL.

Target behavior:

1. Resolve the latest PHP-binaries release manifest.
2. Read `manifest.ReleaseTag`.
3. Read asset names for the requested minor.
4. Download the assets from that exact tag.

Pseudo-flow:

```go
manifest, err := LatestReleaseManifest(ctx)
assets := manifest.Assets[ver]
base := "https://github.com/godismyjudge95/devctl/releases/download/" + manifest.ReleaseTag + "/"
fpmURL := base + assets.FPM
cliURL := base + assets.CLI
```

This eliminates mutable asset URLs.

## 3. Add PHP update metadata support

devctl already stores `LatestVersion` strings for services, but PHP currently sits outside `install.NewRegistry`.

The simplest path is:

1. Keep PHP outside the generic installer registry for now.
2. Add a PHP-specific latest-version check in the API layer or `main.go` startup/update-check flow.
3. For each installed PHP minor:
   - get installed patch version from the current binary using `php -v`
   - get latest patch version for that minor from the manifest
   - if different, set `latest_version` for that `php-fpm-<minor>` service state

### Suggested implementation shape

Option A: enrich PHP service states directly in the PHP/API path.

1. During `handleGetPHPVersions` and service-state enrichment, attach PHP latest-version data from an in-memory cache.
2. Add a periodic PHP release manifest refresh alongside the existing update checkers.

Option B: treat PHP-FPM definitions as updateable services.

1. Add a PHP installer/update abstraction keyed by `php-fpm-8.4`.
2. Wire it into `install.NewRegistry`.
3. Reuse the generic service update pipeline.

Recommendation:

- Start with Option A.
- It is less invasive because PHP already has its own install/uninstall flow and dynamic service registration.
- Once stable, PHP can be moved into a generic updateable-service model if desired.

## 4. Add PHP current patch version parsing

For update comparisons, devctl needs the installed patch version, not just the minor.

Suggested helper:

```go
func InstalledPatchVersion(ver, serverRoot string) (string, error)
```

Implementation:

1. Execute `{serverRoot}/php/{ver}/php -v`
2. Parse `PHP 8.4.19` from the first line
3. Return `8.4.19`

This version should be used as the current `svc.version` for PHP-FPM services wherever practical.

## 5. Add a PHP release metadata cache

To avoid hitting GitHub on every request:

1. Store the latest fetched PHP manifest in memory on the server.
2. Refresh it in the same daily update-check loop used for other services, or in a dedicated PHP checker loop.
3. On successful PHP install/update, optionally re-fetch in the background.

Suggested server fields:

```go
phpLatestTag string
phpLatestVersions map[string]string
```

Or one cached manifest struct if that is cleaner.

## Frontend/UI Changes

The UI already renders:

`Update from {{ svc.version || svc.install_version }} to {{ svc.latest_version }}`

So if backend state becomes:

- `version = 8.4.18`
- `latest_version = 8.4.19`

the tooltip and update text should already improve without a large frontend rewrite.

Potential UI follow-up:

1. In the install picker, consider showing both the supported minor and latest patch version from the manifest.
2. Example:
   - `PHP 8.4`
   - `Installs 8.4.19`

That is optional for the first pass.

## API/State Changes

Suggested behavior for PHP service states:

1. `version` should report the installed patch version, such as `8.4.18`
2. `latest_version` should report the newest patch version for the same minor from the manifest, such as `8.4.19`
3. `update_available` should be `true` when those differ

This will make PHP consistent with the rest of the Services page.

## Test Plan

All runtime tests must use the Incus container workflow. Do not run Go tests on the host.

## Test Categories

1. Pure Go unit tests for parsing and metadata fetching
2. API integration tests in `tests/api/` inside Incus
3. Optional Playwright e2e coverage for the Services page update text

## Unit Tests

Suggested files:

- `php/releases_test.go`
- `php/version_parse_test.go`

Suggested cases:

1. GitHub release list parsing picks newest `php-binaries-*` tag
2. Non-PHP releases such as `v0.7.0` are ignored
3. Manifest JSON parsing succeeds
4. Missing minor version in manifest returns a clear error
5. Installed patch parser extracts `8.4.19` from `php -v`

Host rule:

- Compile only on host with `go test -c`
- Run binaries inside Incus

## Integration Tests In Incus

Suggested API test file:

- `tests/api/php_release_manifest_test.go`

### Core scenarios

#### 1. Install uses newest PHP-binaries release tag

Goal:

- Prove PHP install no longer depends on a mutable `php-binaries-latest` URL.

Approach:

1. Extend the Incus curl shim so it can serve PHP assets for a specific unique release tag.
2. Cache a synthetic or real manifest plus matching binaries under names the shim can map.
3. Trigger PHP 8.4 install.
4. Verify the installed binary hash matches the cached asset for the unique tag.

Recommended shim improvement:

- Match both URL and basename, not basename alone, for PHP assets.
- This avoids ambiguity if multiple PHP release tags are cached in the future.

#### 2. Installed PHP patch version is surfaced correctly

Goal:

- Verify devctl reports current PHP patch version, not just `8.4`.

Steps:

1. Install PHP 8.4 in the test container.
2. Call the services API.
3. Assert the `php-fpm-8.4` service state shows `version = 8.4.x`.

#### 3. Update availability uses manifest patch metadata

Goal:

- Verify the backend compares installed patch to manifest patch.

Steps:

1. Install a known PHP 8.4 binary in the test container.
2. Serve a manifest whose `8.4` value is higher than the installed patch.
3. Trigger update-check refresh.
4. Assert `latest_version` is the higher patch and `update_available` is `true`.

#### 4. PHP update installs the new tag's assets

Goal:

- Verify update replaces the binary with the asset referenced by the newest manifest.

Steps:

1. Install an older PHP 8.4 binary in the container.
2. Make the release manifest point to a newer 8.4 asset.
3. Trigger PHP update.
4. Assert the final binary hash and `php --ri ffi` or `php -v` reflect the newer asset.

## Incus Infrastructure Changes

The current test curl shim is designed around basename-only matching and a single cached PHP payload. That is not sufficient once PHP downloads are tied to unique release tags plus a manifest.

### Changes needed in `scripts/download-artifacts.sh`

1. Cache `php-binaries.json` alongside binaries.
2. Allow multiple cached PHP release sets if needed.
3. Store test fixtures under deterministic filenames that encode the release tag.

Possible naming pattern:

- `php-binaries-20260422.1-php-8.4-cli-linux-x86_64`
- `php-binaries-20260422.1-php-8.4-fpm-linux-x86_64`
- `php-binaries-20260422.1-php-binaries.json`

### Changes needed in `scripts/test-env.sh`

Update the curl shim so it can:

1. Inspect the request URL
2. Detect `releases/download/<tag>/<asset>`
3. Look for a cached file keyed by both `<tag>` and `<asset>`
4. Fall back to real curl if not cached

This is important for realistic testing of the new PHP release model.

## Optional E2E Test

Suggested Playwright test:

- `tests/e2e/php-update.spec.ts`

Scenario:

1. Open Services page
2. Wait for the `PHP 8.4 FPM` row
3. Confirm update badge is visible when test manifest advertises a newer patch
4. Confirm tooltip reads `Update from 8.4.18 to 8.4.19`

This is lower priority than API/integration coverage because the UI mostly reuses existing service update rendering.

## TDD Order

1. Add pure manifest parsing helpers and unit tests
2. Add GitHub release discovery helper and unit tests
3. Extend Incus curl shim and artifact cache to understand unique PHP release tags plus manifest files
4. Write failing API integration test for PHP install using a unique release tag
5. Implement installer resolution via latest `php-binaries-*` manifest
6. Write failing API integration test for PHP patch-version update metadata
7. Implement backend PHP latest-version comparison and state enrichment
8. Add optional Playwright coverage for the Services UI
9. Run full Incus test layers relevant to the touched code

## Verification Commands

### Unit-test compile only on host

```sh
go test -c -o php.test ./php/
incus file push php.test $DEVCTL_CONTAINER/tmp/php.test
incus exec $DEVCTL_CONTAINER -- chmod 755 /tmp/php.test
incus exec $DEVCTL_CONTAINER -- /tmp/php.test -test.v
```

### API integration tests

```sh
make build
make test-env
DEVCTL_BASE_URL=http://127.0.0.1:4000 make test-api
```

### E2E tests

```sh
make build
make test-env
make test-e2e
```

## Files Likely To Change

### Release workflows

- `.github/workflows/build-php.yml`
- `.github/workflows/release.yml`

### PHP backend

- `php/installer.go`
- `php/releases.go`
- `php/releases_test.go`
- `php/versions.go` or a new helper file for installed patch parsing

### Server/API

- `main.go`
- `api/services.go`
- `services/definition.go` if additional PHP state fields are needed

### Test infrastructure

- `scripts/download-artifacts.sh`
- `scripts/test-env.sh`

### Integration and e2e tests

- `tests/api/php_release_manifest_test.go`
- `tests/e2e/php-update.spec.ts`

### Docs and release instructions

- `.agents/skills/create-release/SKILL.md`
- `README.md`

## Open Questions

1. Should PHP update actions be exposed through the generic `/api/services/{id}/update` flow or remain PHP-specific?
2. Should the manifest also include extension-set metadata, such as `ffi: true`, `spx: true`, for debugging and supportability?
3. Should the test artifact cache keep one real PHP release set or multiple fixture sets to cover upgrade transitions more explicitly?

## Recommended Answers

1. Keep PHP install/update logic PHP-specific for the first pass.
2. Yes, add optional extension metadata later, but do not block the initial manifest on it.
3. Support at least two PHP fixture sets in Incus tests so upgrade transitions can be tested cleanly.

## Recommended Implementation Order

1. Fix release model and manifest generation first
2. Implement manifest discovery and installer download changes second
3. Implement PHP patch-version update metadata third
4. Extend Incus test infrastructure fourth
5. Add API and optional e2e tests last, after the release and installer plumbing is stable
