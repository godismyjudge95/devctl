---
name: create-release
description: Full workflow for tagging and publishing a devctl release and/or a PHP binaries release — versioning, release note generation from TODO.md, clearing TODO.md completed items, and the exact git/GitHub CLI steps for each release type.
license: MIT
compatibility: opencode
metadata:
  concerns: release, versioning, changelog, php-binaries
---

## Overview

There are two independent release types in this repo:

| Release type | Tag format | GitHub Actions triggered | Binary attached |
|---|---|---|---|
| devctl binary | `v1.2.3` | `release.yml` | `devctl` (linux-x86_64) |
| PHP binaries | `php-binaries-YYYYMMDD.N` | `build-php.yml` | `php-{ver}-{cli,fpm}-linux-x86_64` for 7.0–8.5 plus `php-binaries.json` |

They are fully independent — you can do either or both. PHP binaries now use unique immutable tags, and devctl discovers the newest `php-binaries-*` release plus its `php-binaries.json` manifest at install/update time.

---

## 1. devctl Release

### 1.1 Determine the version

Use [semver](https://semver.org/):
- **Patch** (`v1.0.x`) — bug fixes only, no new features.
- **Minor** (`v1.x.0`) — new user-visible features, backward-compatible.
- **Major** (`vx.0.0`) — breaking changes.

Check the last tag to determine the next version:
```sh
git tag --sort=-version:refname | head -5
```

### 1.2 Generate release notes from TODO.md

Release notes come from **everything currently in the Completed section** of `TODO.md`. The Completed section acts as a release buffer — items accumulate there between releases and are cleared on each release.

Convert each item into a concise one-line bullet. Strip the `*(completed …)*` timestamps — they're internal. Format:

```markdown
## What's changed

- Short description of change one
- Short description of change two
- Bug fix: brief description

## Full changelog
https://github.com/godismyjudge95/devctl/compare/vPREV...vNEW
```

### 1.3 Clear the Completed section and commit TODO.md

After drafting the release notes, **remove all items from the Completed section** of `TODO.md`, leaving the section header empty. Also remove any stale/abandoned items from the backlog if they are no longer relevant.

Commit the result:

```sh
git add TODO.md
git commit -m "chore: prepare vX.Y.Z release"
```

### 1.4 Tag and push

```sh
# Make sure the tree is clean and on main
git status
git checkout main
git pull

# Tag (after the TODO.md commit above)
git tag vX.Y.Z
git push origin main vX.Y.Z
```

### 1.5 Publish the GitHub release

Use the `gh` CLI to create the release in one step. Write the release notes to a temp file first, then pass it to `gh`:

```sh
cat > /tmp/release-notes.md << 'EOF'
## What's changed

- Short description of change one
- Short description of change two
- Bug fix: brief description

## Full changelog
https://github.com/godismyjudge95/devctl/compare/vPREV...vNEW
EOF

gh release create vX.Y.Z \
  --title "vX.Y.Z" \
  --notes-file /tmp/release-notes.md
```

`release.yml` will fire automatically and attach the `devctl` binary.

> **Note:** If you need to move the tag after creation (e.g. because a commit was made after tagging), delete and re-create the tag, then re-publish the release — moving a tag puts the GitHub release back into draft:
> ```sh
> git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z
> git tag vX.Y.Z && git push origin vX.Y.Z
> gh release edit vX.Y.Z --draft=false
> ```

---

## 2. PHP Binaries Release

Release PHP binaries **separately** from the devctl binary. Do this when:
- A new static PHP version is needed (e.g. PHP 8.5 support added).
- The extension set changes (e.g. new extension added to `build-php.yml`).
- Upstream static-php-cli has meaningful updates worth picking up.

PHP binaries use unique immutable tags such as `php-binaries-20260422.1`. Publishing a new PHP binaries release does not replace older ones; devctl discovers the newest matching release automatically.

**Compile 8.x locally first.** GitHub Actions is a terrible place to iterate on static-php-cli failures. Prove the current extension list with the Docker mirror of the compile cells before tagging:

```sh
scripts/local-build-php8.sh 8.4
```

PHP 8.0 **is** compiled. Prove it with `scripts/local-build-php8.sh 8.0` after changing `scripts/patch-spc-for-php80.sh`. Keep the 8.0 pins (libxml2 2.12.10, libxslt 1.1.39, ICU 70.1, ImageMagick 7.1.2-30, imagick 3.8.1). After download, drop the zstd and brotli stub files (PHP 8.0 gen_stub cannot parse `const`; `zstd.c` / `brotli.c` already ship inline arginfo). Do not add protobuf or opentelemetry on 8.0 (current PECL needs PHP 8.1+). Do not add pcov on 8.0 (PECL cfg/704 zend_cfg fails the in-tree static make). Fix any 8.0–8.5 compile errors in `build-php.yml` / `scripts/php8-exts.sh` / `scripts/patch-spc-for-php8.sh` / `scripts/patch-spc-for-php80.sh` / the local script, then tag. The 8.1–8.5 list must keep **mysqli**, **sodium**, **spx**, and **pcov**. The 8.0 list must keep **mysqli**, **sodium**, and **spx**. Keep the libaom v3.12.1 and libevent 2.1.12 pins in `patch-spc-for-php8.sh` (current spc defaults fail the imagick/event compile). static-php-cli marks pcov shared-only; the patch enables a static compile (extract into `php-src/ext/pcov`, plus `config/spc-pcov-static.php` so config.m4 does not call php-config or overwrite `PHP_VERSION`).

### 2.1 Choose the PHP binaries tag

Use a unique immutable tag for each PHP binaries release:

```sh
PHP_TAG="php-binaries-$(date +%Y%m%d).1"
```

### 2.2 Create the new tag

```sh
# Point the tag at whatever commit should back the new binaries (usually HEAD of main)
git tag "$PHP_TAG"
git push origin "$PHP_TAG"
```

### 2.3 Publish the GitHub release

Use the `gh` CLI:

```sh
gh release create "$PHP_TAG" \
  --title "PHP Binaries — ${PHP_TAG}" \
  --notes "Updated to static-php-cli main as of $(date +%Y-%m-%d); <describe what changed>"
```

`build-php.yml` fires automatically (the tag starts with `php-binaries`), builds supported PHP minors in parallel (legacy 7.x with mysqli/pdo_pgsql/pdo_sqlite, compiled 8.0–8.5 with the custom set including mysqli/sodium/spx/pcov), and attaches the binaries plus `php-binaries.json` to this release.

### 2.4 Verify

After the CI run completes, check the release page has the expected assets (minors that failed to build may be absent — check the workflow run):
```
php-7.0-cli-linux-x86_64 / php-7.0-fpm-linux-x86_64
php-7.2-cli-linux-x86_64 / php-7.2-fpm-linux-x86_64
php-7.4-cli-linux-x86_64 / php-7.4-fpm-linux-x86_64
php-8.0-cli-linux-x86_64 / php-8.0-fpm-linux-x86_64
php-8.1-cli-linux-x86_64 / php-8.1-fpm-linux-x86_64
php-8.2-cli-linux-x86_64 / php-8.2-fpm-linux-x86_64
php-8.3-cli-linux-x86_64 / php-8.3-fpm-linux-x86_64
php-8.4-cli-linux-x86_64 / php-8.4-fpm-linux-x86_64
php-8.5-cli-linux-x86_64 / php-8.5-fpm-linux-x86_64
php-binaries.json
```

Test that the manifest URL resolves correctly:
```sh
curl -sIL "https://github.com/godismyjudge95/devctl/releases/download/${PHP_TAG}/php-binaries.json" | grep -i "content-length\|location\|HTTP/"
```

---

## Checklist — devctl release

- [ ] Determine semver version
- [ ] Read the full Completed section of `TODO.md` and draft release notes from it
- [ ] Clear all items from the Completed section of `TODO.md`
- [ ] Remove stale backlog items if needed
- [ ] `git add TODO.md && git commit -m "chore: prepare vX.Y.Z release"`
- [ ] `git tag vX.Y.Z && git push origin main vX.Y.Z`
- [ ] `gh release create vX.Y.Z --title "vX.Y.Z" --notes-file /tmp/release-notes.md` — `release.yml` attaches binary automatically

## Checklist — PHP binaries release

- [ ] Choose a unique `php-binaries-YYYYMMDD.N` tag
- [ ] `git tag "$PHP_TAG" && git push origin "$PHP_TAG"`
- [ ] `gh release create "$PHP_TAG" --title "PHP Binaries — ${PHP_TAG}" --notes "<what changed>"`
- [ ] Wait for `build-php.yml` to complete
- [ ] Verify binary assets plus `php-binaries.json` are attached (see 2.4)
