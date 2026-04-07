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

There is **no version number** for PHP binaries — the installer always pulls from the fixed `php-binaries-latest` tag. Publishing a new PHP binaries release replaces the previous one.

### 2.1 Delete the old `php-binaries-latest` release and tag

The fixed tag must be re-created to point at the new commit. Delete the release and tag via `gh` CLI, then clean up locally:

```sh
# Delete the GitHub release (this does NOT delete the tag)
gh release delete php-binaries-latest --yes

# Delete the remote tag
git push origin :refs/tags/php-binaries-latest

# Delete the local tag
git tag -d php-binaries-latest
```

### 2.2 Create the new tag

```sh
# Point the tag at whatever commit should back the new binaries (usually HEAD of main)
git tag php-binaries-latest
git push origin php-binaries-latest
```

### 2.3 Publish the GitHub release

Use the `gh` CLI:

```sh
gh release create php-binaries-latest \
  --title "PHP Binaries — $(date +%Y-%m-%d)" \
  --notes "Updated to static-php-cli main as of $(date +%Y-%m-%d); <describe what changed>"
```

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
- [ ] Read the full Completed section of `TODO.md` and draft release notes from it
- [ ] Clear all items from the Completed section of `TODO.md`
- [ ] Remove stale backlog items if needed
- [ ] `git add TODO.md && git commit -m "chore: prepare vX.Y.Z release"`
- [ ] `git tag vX.Y.Z && git push origin main vX.Y.Z`
- [ ] `gh release create vX.Y.Z --title "vX.Y.Z" --notes-file /tmp/release-notes.md` — `release.yml` attaches binary automatically

## Checklist — PHP binaries release

- [ ] `gh release delete php-binaries-latest --yes`
- [ ] `git push origin :refs/tags/php-binaries-latest && git tag -d php-binaries-latest`
- [ ] `git tag php-binaries-latest && git push origin php-binaries-latest`
- [ ] `gh release create php-binaries-latest --title "PHP Binaries — <date>" --notes "<what changed>"`
- [ ] Wait for `build-php.yml` to complete
- [ ] Verify all 8 binary assets are attached to the release
