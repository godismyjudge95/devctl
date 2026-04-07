---
name: create-release
description: Full workflow for tagging and publishing a devctl release and/or a PHP binaries release — versioning, release note generation from TODO.md, cleaning up TODO.md before tagging, and the exact git/GitHub steps for each release type.
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
| PHP binaries | `php-binaries-latest` (fixed) | `build-php.yml` | `php-{ver}-{cli,fpm}-linux-x86_64` for 8.1–8.4 |

They are fully independent — you can do either or both. The PHP binaries release uses a **fixed tag** (`php-binaries-latest`) so the installer in `php/installer.go` always resolves to the same URL regardless of which devctl version is current.

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

Release notes come from the **Completed** section of `TODO.md`. Include only items completed since the last release (check the timestamps — completed items are tagged with a date like `*(completed 2026-04-06)*`).

Format them as a Markdown bullet list for the GitHub release body:

```markdown
## What's changed

- Short description of change one
- Short description of change two
- Bug fix: brief description

## Full changelog
https://github.com/godismyjudge95/devctl/compare/vPREV...vNEW
```

Keep each bullet to one line. Strip the `*(completed …)*` timestamps — they're internal.

### 1.3 Clean up TODO.md before tagging

Before creating the tag:
1. Move any completed items that are **not yet in the Completed section** down to it.
2. Remove any stale/abandoned items from the backlog if they are no longer relevant.
3. Commit the updated `TODO.md` as part of the release commit (or as its own commit immediately before the tag).

### 1.4 Tag and push

```sh
# Make sure the tree is clean and on main
git status
git checkout main
git pull

# Commit any last-minute TODO.md / README updates
git add TODO.md README.md
git commit -m "chore: prepare vX.Y.Z release"

# Tag
git tag vX.Y.Z
git push origin main vX.Y.Z
```

### 1.5 Publish the GitHub release

1. Go to **GitHub → Releases → Draft a new release**.
2. Choose tag `vX.Y.Z`.
3. Set the title to `vX.Y.Z`.
4. Paste the release notes generated in step 1.2.
5. Click **Publish release**.

`release.yml` will fire automatically and attach the `devctl` binary.

---

## 2. PHP Binaries Release

Release PHP binaries **separately** from the devctl binary. Do this when:
- A new static PHP version is needed (e.g. PHP 8.5 support added).
- The extension set changes (e.g. new extension added to `build-php.yml`).
- Upstream static-php-cli has meaningful updates worth picking up.

There is **no version number** for PHP binaries — the installer always pulls from the fixed `php-binaries-latest` tag. Publishing a new PHP binaries release replaces the previous one.

### 2.1 Delete the old `php-binaries-latest` release and tag

The fixed tag must be re-created to point at the new commit. Do this on GitHub first to avoid ref conflicts:

1. Go to **GitHub → Releases**, find the release titled `php-binaries-latest`, click **Delete release** (trash icon). This removes the release but not the tag.
2. Go to **GitHub → Tags**, find `php-binaries-latest`, click the `…` menu → **Delete tag**.

Then delete locally and on the remote:
```sh
git tag -d php-binaries-latest
git push origin :refs/tags/php-binaries-latest
```

### 2.2 Create the new tag

```sh
# Point the tag at whatever commit should back the new binaries (usually HEAD of main)
git tag php-binaries-latest
git push origin php-binaries-latest
```

### 2.3 Publish the GitHub release

1. Go to **GitHub → Releases → Draft a new release**.
2. Choose tag `php-binaries-latest`.
3. Set the title to `PHP Binaries — <date>` (e.g. `PHP Binaries — 2026-04-06`).
4. Write a short description of what changed (e.g. "Updated to static-php-cli main as of 2026-04-06; added zstd extension").
5. Click **Publish release**.

`build-php.yml` fires automatically (the tag starts with `php-binaries`), builds all four PHP versions in parallel, and attaches the eight binaries to this release.

### 2.4 Verify

After the CI run completes, check the release page has all eight assets:
```
php-8.1-cli-linux-x86_64
php-8.1-fpm-linux-x86_64
php-8.2-cli-linux-x86_64
php-8.2-fpm-linux-x86_64
php-8.3-cli-linux-x86_64
php-8.3-fpm-linux-x86_64
php-8.4-cli-linux-x86_64
php-8.4-fpm-linux-x86_64
```

Test that the installer URL resolves correctly:
```sh
curl -sIL https://github.com/godismyjudge95/devctl/releases/download/php-binaries-latest/php-8.3-cli-linux-x86_64 | grep -i "content-length\|location\|HTTP/"
```

---

## Checklist — devctl release

- [ ] Determine semver version
- [ ] Draft release notes from `TODO.md` Completed section (items since last release)
- [ ] Move any un-moved completed items into the Completed section of `TODO.md`
- [ ] Remove stale backlog items if needed
- [ ] Commit `TODO.md` (and `README.md` if updated)
- [ ] `git tag vX.Y.Z && git push origin main vX.Y.Z`
- [ ] Publish GitHub release — `release.yml` attaches binary automatically

## Checklist — PHP binaries release

- [ ] Delete old `php-binaries-latest` GitHub release (UI)
- [ ] Delete old `php-binaries-latest` tag (UI + `git tag -d` + `git push origin :refs/tags/php-binaries-latest`)
- [ ] `git tag php-binaries-latest && git push origin php-binaries-latest`
- [ ] Publish GitHub release titled `PHP Binaries — <date>`
- [ ] Wait for `build-php.yml` to complete
- [ ] Verify all 8 binary assets are attached to the release
