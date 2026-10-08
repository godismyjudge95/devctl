// Package githubapi is the shared GitHub Releases client used by service
// installers, helper downloads, and self-update.
package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/danielgormly/devctl/internal/httplog"
)

// Token returns a GitHub token from the environment, if one is set.
// Checked in order: DEVCTL_GITHUB_TOKEN, GH_TOKEN, GITHUB_TOKEN.
func Token() string {
	for _, key := range []string{"DEVCTL_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// SetRequestHeaders sets GitHub REST Accept, User-Agent, and optional Authorization headers.
func SetRequestHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")
	if tok := Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// HTTPStatusError reads a bounded response body and returns an error that
// includes the status code and GitHub's message field when present.
func HTTPStatusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		msg = parsed.Message
	}
	if msg != "" {
		return fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode, msg)
	}
	return fmt.Errorf("%s: HTTP %d", op, resp.StatusCode)
}

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
	SetRequestHeaders(req)

	done := httplog.LogGitHubRequestStart(req.Method, url)
	resp, err := http.DefaultClient.Do(req)
	done(resp, err)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", HTTPStatusError("github version check "+ownerRepo, resp)
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
