package php

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/httplog"
)

const (
	githubRepo          = "godismyjudge95/devctl"
	manifestAssetName   = "php-binaries.json"
	releaseTagPrefix    = "php-binaries-"
	releaseFetchTimeout = 30 * time.Second
)

type ReleaseManifest struct {
	ReleaseTag  string                   `json:"release_tag"`
	BuiltAt     string                   `json:"built_at"`
	PHPVersions map[string]string        `json:"php_versions"`
	Assets      map[string]ReleaseAssets `json:"assets"`
}

type ReleaseAssets struct {
	CLI string `json:"cli"`
	FPM string `json:"fpm"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func LatestReleaseTag(ctx context.Context) (string, error) {
	releases, err := listReleases(ctx)
	if err != nil {
		return "", err
	}
	// Prefer immutable dated tags (php-binaries-YYYYMMDD.N) over legacy
	// php-binaries-latest so lexicographic "latest" never outranks real builds.
	var versioned, legacy []string
	for _, rel := range releases {
		if !strings.HasPrefix(rel.TagName, releaseTagPrefix) {
			continue
		}
		if isVersionedPHPReleaseTag(rel.TagName) {
			versioned = append(versioned, rel.TagName)
		} else {
			legacy = append(legacy, rel.TagName)
		}
	}
	if len(versioned) > 0 {
		sort.Slice(versioned, func(i, j int) bool {
			return comparePHPReleaseTags(versioned[i], versioned[j]) < 0
		})
		return versioned[len(versioned)-1], nil
	}
	if len(legacy) > 0 {
		sort.Strings(legacy)
		return legacy[len(legacy)-1], nil
	}
	return "", fmt.Errorf("no %s releases found", releaseTagPrefix)
}

// isVersionedPHPReleaseTag reports whether tag is an immutable dated release
// such as php-binaries-20260422.1 (as opposed to php-binaries-latest).
func isVersionedPHPReleaseTag(tag string) bool {
	_, _, ok := parsePHPReleaseTag(tag)
	return ok
}

// parsePHPReleaseTag extracts YYYYMMDD and N from php-binaries-YYYYMMDD.N.
func parsePHPReleaseTag(tag string) (date int, n int, ok bool) {
	rest := strings.TrimPrefix(tag, releaseTagPrefix)
	if rest == "" || rest == tag {
		return 0, 0, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	for _, ch := range parts[0] {
		if ch < '0' || ch > '9' {
			return 0, 0, false
		}
	}
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return 0, 0, false
		}
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &date); err != nil || date <= 0 {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &n); err != nil || n < 0 {
		return 0, 0, false
	}
	return date, n, true
}

// comparePHPReleaseTags orders php-binaries-YYYYMMDD.N tags by date then N.
// Lexicographic string sort is wrong: php-binaries-20260723.9 > .24 as strings.
func comparePHPReleaseTags(a, b string) int {
	da, na, aOK := parsePHPReleaseTag(a)
	db, nb, bOK := parsePHPReleaseTag(b)
	switch {
	case aOK && bOK:
		if da != db {
			return da - db
		}
		return na - nb
	case aOK:
		return 1
	case bOK:
		return -1
	default:
		return strings.Compare(a, b)
	}
}

func FetchReleaseManifest(ctx context.Context, tag string) (*ReleaseManifest, error) {
	release, err := getReleaseByTag(ctx, tag)
	if err != nil {
		return nil, err
	}
	var manifestURL string
	for _, asset := range release.Assets {
		if asset.Name == manifestAssetName {
			manifestURL = asset.BrowserDownloadURL
			break
		}
	}
	// Older php-binaries-latest releases ship CLI/FPM assets without a
	// php-binaries.json. Synthesize a manifest from asset filenames so install
	// still works until a versioned release with a real manifest is published.
	if manifestURL == "" {
		manifest, synthErr := synthesizeManifestFromAssets(tag, release)
		if synthErr != nil {
			return nil, fmt.Errorf("php release %s missing %s: %w", tag, manifestAssetName, synthErr)
		}
		return manifest, nil
	}

	req, err := newGitHubRequest(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	done := httplog.LogGitHubRequestStart(req.Method, manifestURL)
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return nil, fmt.Errorf("fetch php manifest %s: %w", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch php manifest %s: HTTP %d", tag, resp.StatusCode)
	}

	var manifest ReleaseManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode php manifest %s: %w", tag, err)
	}
	if manifest.ReleaseTag == "" {
		manifest.ReleaseTag = tag
	}
	if manifest.ReleaseTag != tag {
		return nil, fmt.Errorf("php manifest release_tag %q does not match tag %q", manifest.ReleaseTag, tag)
	}
	if manifest.PHPVersions == nil {
		manifest.PHPVersions = map[string]string{}
	}
	if manifest.Assets == nil {
		manifest.Assets = map[string]ReleaseAssets{}
	}
	return &manifest, nil
}

// synthesizeManifestFromAssets builds a ReleaseManifest by scanning release
// assets named php-{minor}-{cli|fpm}-linux-x86_64. Patch versions are unknown
// without a real manifest, so PHPVersions is left empty.
func synthesizeManifestFromAssets(tag string, release *githubRelease) (*ReleaseManifest, error) {
	assets := map[string]ReleaseAssets{}
	for _, asset := range release.Assets {
		minor, kind, ok := parsePHPBinaryAssetName(asset.Name)
		if !ok {
			continue
		}
		entry := assets[minor]
		switch kind {
		case "cli":
			entry.CLI = asset.Name
		case "fpm":
			entry.FPM = asset.Name
		}
		assets[minor] = entry
	}
	// Keep only minors that have both CLI and FPM.
	complete := map[string]ReleaseAssets{}
	for minor, a := range assets {
		if a.CLI != "" && a.FPM != "" {
			complete[minor] = a
		}
	}
	if len(complete) == 0 {
		return nil, fmt.Errorf("no php-{ver}-{cli|fpm}-linux-x86_64 assets found")
	}
	return &ReleaseManifest{
		ReleaseTag:  tag,
		PHPVersions: map[string]string{},
		Assets:      complete,
	}, nil
}

// parsePHPBinaryAssetName extracts minor version and kind ("cli"/"fpm") from
// names like php-8.4-cli-linux-x86_64. Returns ok=false when the name does not
// match.
func parsePHPBinaryAssetName(name string) (minor, kind string, ok bool) {
	// php-<minor>-<kind>-linux-x86_64
	const suffix = "-linux-x86_64"
	if !strings.HasPrefix(name, "php-") || !strings.HasSuffix(name, suffix) {
		return "", "", false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(name, "php-"), suffix) // e.g. "8.4-cli"
	// Split on last hyphen so minors like "8.4" work; kind is the final segment.
	i := strings.LastIndex(mid, "-")
	if i <= 0 || i == len(mid)-1 {
		return "", "", false
	}
	minor = mid[:i]
	kind = mid[i+1:]
	if kind != "cli" && kind != "fpm" {
		return "", "", false
	}
	// Basic minor sanity: must look like N.N
	if !strings.Contains(minor, ".") {
		return "", "", false
	}
	return minor, kind, true
}

func LatestReleaseManifest(ctx context.Context) (*ReleaseManifest, error) {
	tag, err := LatestReleaseTag(ctx)
	if err != nil {
		return nil, err
	}
	return FetchReleaseManifest(ctx, tag)
}

func AssetURLsForMinor(ctx context.Context, minor string) (cliURL, fpmURL string, manifest *ReleaseManifest, err error) {
	manifest, err = LatestReleaseManifest(ctx)
	if err != nil {
		return "", "", nil, err
	}
	assets, ok := manifest.Assets[minor]
	if !ok {
		return "", "", nil, fmt.Errorf("php %s is not available in release %s (available: %s)",
			minor, manifest.ReleaseTag, strings.Join(sortedAssetMinors(manifest), ", "))
	}
	if assets.CLI == "" || assets.FPM == "" {
		return "", "", nil, fmt.Errorf("php release %s has incomplete assets for %s", manifest.ReleaseTag, minor)
	}
	base := githubDownloadBase() + "/" + manifest.ReleaseTag + "/"
	return base + assets.CLI, base + assets.FPM, manifest, nil
}

func sortedAssetMinors(manifest *ReleaseManifest) []string {
	if manifest == nil || len(manifest.Assets) == 0 {
		return nil
	}
	minors := make([]string, 0, len(manifest.Assets))
	for m := range manifest.Assets {
		minors = append(minors, m)
	}
	sort.Strings(minors)
	return minors
}

func githubAPIBase() string {
	if v := os.Getenv("DEVCTL_PHP_RELEASES_API_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.github.com/repos/" + githubRepo
}

func githubDownloadBase() string {
	if v := os.Getenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://github.com/" + githubRepo + "/releases/download"
}

func listReleases(ctx context.Context) ([]githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, releaseFetchTimeout)
	defer cancel()
	req, err := newGitHubRequest(ctx, githubAPIBase()+"/releases")
	if err != nil {
		return nil, err
	}
	done := httplog.LogGitHubRequestStart(req.Method, req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return nil, fmt.Errorf("list php releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list php releases: HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode php releases: %w", err)
	}
	return releases, nil
}

func getReleaseByTag(ctx context.Context, tag string) (*githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, releaseFetchTimeout)
	defer cancel()
	req, err := newGitHubRequest(ctx, githubAPIBase()+"/releases/tags/"+tag)
	if err != nil {
		return nil, err
	}
	done := httplog.LogGitHubRequestStart(req.Method, req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return nil, fmt.Errorf("get php release %s: %w", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get php release %s: HTTP %d", tag, resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode php release %s: %w", tag, err)
	}
	return &rel, nil
}

func newGitHubRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")
	return req, nil
}
