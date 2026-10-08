package php

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/danielgormly/devctl/dist"
	"github.com/danielgormly/devctl/internal/githubapi"
	"github.com/danielgormly/devctl/internal/httplog"
)

const (
	githubRepo          = "godismyjudge95/devctl"
	manifestAssetName   = "php-binaries.json"
	releaseTagPrefix    = "php-binaries-"
	releaseFetchTimeout = 30 * time.Second
)

type ReleaseManifest struct {
	ReleaseTag     string                              `json:"release_tag"`
	BuiltAt        string                              `json:"built_at"`
	PHPVersions    map[string]string                   `json:"php_versions"`
	Assets         map[string]ReleaseAssets            `json:"assets"`
	PlatformAssets map[string]map[string]ReleaseAssets `json:"platform_assets,omitempty"`
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
	if useDirectManifestDownload() {
		jsonURL := githubDownloadBase() + "/" + tag + "/" + manifestAssetName
		manifest, err := fetchManifestJSON(ctx, jsonURL, tag)
		if err == nil {
			return manifest, nil
		}
		if !isHTTPStatus(err, http.StatusNotFound) {
			return nil, err
		}
	}

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

	return fetchManifestJSON(ctx, manifestURL, tag)
}

func fetchManifestJSON(ctx context.Context, manifestURL, tag string) (*ReleaseManifest, error) {
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
		return nil, githubapi.HTTPStatusError(fmt.Sprintf("fetch php manifest %s", tag), resp)
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
	if manifest.PlatformAssets == nil {
		manifest.PlatformAssets = map[string]map[string]ReleaseAssets{}
	}
	return &manifest, nil
}

// useDirectManifestDownload reports whether php-binaries.json should be fetched
// from the GitHub download URL (github.com, not api.github.com). Unit tests that
// mock only the API leave DOWNLOAD_BASE unset, so they keep the API path.
func useDirectManifestDownload() bool {
	if os.Getenv("DEVCTL_PHP_RELEASES_DOWNLOAD_BASE") != "" {
		return true
	}
	return os.Getenv("DEVCTL_PHP_RELEASES_API_BASE") == ""
}

func isHTTPStatus(err error, code int) bool {
	return err != nil && strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", code))
}

// synthesizeManifestFromAssets builds a ReleaseManifest by scanning release
// assets named php-{minor}-{cli|fpm}-{platform}. Patch versions are unknown
// without a real manifest, so PHPVersions is left empty.
func synthesizeManifestFromAssets(tag string, release *githubRelease) (*ReleaseManifest, error) {
	type key struct {
		minor, platform string
	}
	collected := map[key]ReleaseAssets{}
	for _, asset := range release.Assets {
		minor, kind, platform, ok := parsePHPBinaryAsset(asset.Name)
		if !ok {
			continue
		}
		k := key{minor, platform}
		entry := collected[k]
		switch kind {
		case "cli":
			entry.CLI = asset.Name
		case "fpm":
			entry.FPM = asset.Name
		}
		collected[k] = entry
	}
	platformAssets := map[string]map[string]ReleaseAssets{}
	assets := map[string]ReleaseAssets{}
	for k, a := range collected {
		if a.CLI == "" || a.FPM == "" {
			continue
		}
		if platformAssets[k.platform] == nil {
			platformAssets[k.platform] = map[string]ReleaseAssets{}
		}
		platformAssets[k.platform][k.minor] = a
		if k.platform == "linux-x86_64" {
			assets[k.minor] = a
		}
	}
	if len(assets) == 0 && len(platformAssets) == 0 {
		return nil, fmt.Errorf("no php-{ver}-{cli|fpm}-{platform} assets found")
	}
	return &ReleaseManifest{
		ReleaseTag:     tag,
		PHPVersions:    map[string]string{},
		Assets:         assets,
		PlatformAssets: platformAssets,
	}, nil
}

var phpBinaryAssetRe = regexp.MustCompile(`^php-(\d+\.\d+)-(cli|fpm)-(linux-x86_64|macos-aarch64)$`)

func parsePHPBinaryAsset(name string) (minor, kind, platform string, ok bool) {
	m := phpBinaryAssetRe.FindStringSubmatch(name)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// parsePHPBinaryAssetName extracts minor version and kind ("cli"/"fpm") from
// names like php-8.4-cli-linux-x86_64. Returns ok=false when the name does not
// match.
func parsePHPBinaryAssetName(name string) (minor, kind string, ok bool) {
	minor, kind, _, ok = parsePHPBinaryAsset(name)
	return
}

func LatestReleaseManifest(ctx context.Context) (*ReleaseManifest, error) {
	tag, err := LatestReleaseTag(ctx)
	if err != nil {
		return nil, err
	}
	return FetchReleaseManifest(ctx, tag)
}

func phpPlatformToken() string {
	a, err := dist.For("php")
	if err != nil {
		return "linux-x86_64"
	}
	return a.Token
}

// Lookup returns CLI/FPM asset names for minor on platform.
// New manifests use platform_assets. Old manifests only fill assets (linux).
func (m *ReleaseManifest) Lookup(minor, platform string) (ReleaseAssets, bool) {
	if m == nil {
		return ReleaseAssets{}, false
	}
	if len(m.PlatformAssets) > 0 {
		a, ok := m.PlatformAssets[platform][minor]
		if ok && a.CLI != "" && a.FPM != "" {
			return a, true
		}
		return ReleaseAssets{}, false
	}
	if platform != "linux-x86_64" {
		return ReleaseAssets{}, false
	}
	a, ok := m.Assets[minor]
	if !ok || a.CLI == "" || a.FPM == "" {
		return ReleaseAssets{}, false
	}
	return a, true
}

func AssetURLsForMinor(ctx context.Context, minor string) (cliURL, fpmURL string, manifest *ReleaseManifest, err error) {
	return assetURLsForMinorOn(ctx, minor, phpPlatformToken())
}

func assetURLsForMinorOn(ctx context.Context, minor, platform string) (cliURL, fpmURL string, manifest *ReleaseManifest, err error) {
	manifest, err = LatestReleaseManifest(ctx)
	if err != nil {
		return "", "", nil, err
	}
	assets, ok := manifest.Lookup(minor, platform)
	if !ok {
		return "", "", nil, fmt.Errorf("php %s is not available for %s in release %s (available: %s)",
			minor, platform, manifest.ReleaseTag, strings.Join(sortedAssetMinorsFor(manifest, platform), ", "))
	}
	base := githubDownloadBase() + "/" + manifest.ReleaseTag + "/"
	return base + assets.CLI, base + assets.FPM, manifest, nil
}

func sortedAssetMinors(manifest *ReleaseManifest) []string {
	return sortedAssetMinorsFor(manifest, phpPlatformToken())
}

func sortedAssetMinorsFor(manifest *ReleaseManifest, platform string) []string {
	if manifest == nil {
		return nil
	}
	var minors []string
	if len(manifest.PlatformAssets) > 0 {
		for m := range manifest.PlatformAssets[platform] {
			minors = append(minors, m)
		}
	} else if platform == "linux-x86_64" {
		for m := range manifest.Assets {
			minors = append(minors, m)
		}
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

func githubAtomURL() string {
	if v := os.Getenv("DEVCTL_PHP_RELEASES_ATOM_URL"); v != "" {
		return v
	}
	if os.Getenv("DEVCTL_PHP_RELEASES_API_BASE") != "" {
		return ""
	}
	return "https://github.com/" + githubRepo + "/releases.atom"
}

func listReleases(ctx context.Context) ([]githubRelease, error) {
	releases, err := listReleasesFromAPI(ctx)
	if err == nil {
		return releases, nil
	}
	if !isHTTPStatus(err, http.StatusForbidden) && !isHTTPStatus(err, http.StatusTooManyRequests) {
		return nil, err
	}
	fallback, atomErr := listReleasesFromAtom(ctx)
	if atomErr == nil && len(fallback) > 0 {
		return fallback, nil
	}
	return nil, err
}

func listReleasesFromAPI(ctx context.Context) ([]githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, releaseFetchTimeout)
	defer cancel()
	req, err := newGitHubRequest(ctx, githubAPIBase()+"/releases?per_page=100")
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
		return nil, githubapi.HTTPStatusError("list php releases", resp)
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode php releases: %w", err)
	}
	return releases, nil
}

var githubReleaseTagPathRe = regexp.MustCompile(`/releases/tag/([A-Za-z0-9._-]+)`)

func listReleasesFromAtom(ctx context.Context) ([]githubRelease, error) {
	atomURL := githubAtomURL()
	if atomURL == "" {
		return nil, fmt.Errorf("list php releases: no atom feed URL")
	}
	ctx, cancel := context.WithTimeout(ctx, releaseFetchTimeout)
	defer cancel()
	req, err := newGitHubRequest(ctx, atomURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	done := httplog.LogGitHubRequestStart(req.Method, req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return nil, fmt.Errorf("list php releases atom: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, githubapi.HTTPStatusError("list php releases atom", resp)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read php releases atom: %w", err)
	}
	seen := map[string]bool{}
	var releases []githubRelease
	for _, m := range githubReleaseTagPathRe.FindAllSubmatch(body, -1) {
		tag := string(m[1])
		if seen[tag] {
			continue
		}
		seen[tag] = true
		releases = append(releases, githubRelease{TagName: tag})
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
		return nil, githubapi.HTTPStatusError(fmt.Sprintf("get php release %s", tag), resp)
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
	githubapi.SetRequestHeaders(req)
	return req, nil
}
