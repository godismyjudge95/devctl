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
	tags := make([]string, 0, len(releases))
	for _, rel := range releases {
		if strings.HasPrefix(rel.TagName, releaseTagPrefix) {
			tags = append(tags, rel.TagName)
		}
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no %s releases found", releaseTagPrefix)
	}
	sort.Strings(tags)
	return tags[len(tags)-1], nil
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
	if manifestURL == "" {
		return nil, fmt.Errorf("php release %s missing %s", tag, manifestAssetName)
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
		return "", "", nil, fmt.Errorf("php release %s missing assets for %s", manifest.ReleaseTag, minor)
	}
	if assets.CLI == "" || assets.FPM == "" {
		return "", "", nil, fmt.Errorf("php release %s has incomplete assets for %s", manifest.ReleaseTag, minor)
	}
	base := githubDownloadBase() + "/" + manifest.ReleaseTag + "/"
	return base + assets.CLI, base + assets.FPM, manifest, nil
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
