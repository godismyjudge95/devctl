// Package githubapi is the shared GitHub Releases client used by service
// installers, helper downloads, and self-update.
package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgormly/devctl/internal/httplog"
)

// LatestTag queries the GitHub Releases API and returns the tag_name of the
// latest release for ownerRepo (e.g. "caddyserver/caddy"). The returned string
// includes any "v" prefix present in the tag (e.g. "v2.10.0").
func LatestTag(ctx context.Context, ownerRepo string) (string, error) {
	url := "https://api.github.com/repos/" + ownerRepo + "/releases/latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")

	done := httplog.LogGitHubRequestStart(req.Method, url)
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github version check %s: HTTP %d", ownerRepo, resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("github version check %s: decode: %w", ownerRepo, err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("github version check %s: empty tag_name", ownerRepo)
	}
	return payload.TagName, nil
}
