package httplog

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func IsGitHubURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	return host == "github.com" || host == "api.github.com"
}

func LogGitHubRequestStart(method, rawURL string) func(*http.Response, error) {
	if !IsGitHubURL(rawURL) {
		return func(*http.Response, error) {}
	}

	started := time.Now()
	log.Printf("github: request method=%s url=%s", method, rawURL)

	return func(resp *http.Response, err error) {
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			log.Printf("github: response method=%s url=%s duration=%s error=%v", method, rawURL, elapsed, err)
			return
		}
		if resp == nil {
			log.Printf("github: response method=%s url=%s duration=%s status=unknown", method, rawURL, elapsed)
			return
		}
		log.Printf("github: response method=%s url=%s duration=%s status=%d", method, rawURL, elapsed, resp.StatusCode)
	}
}

func LogGitHubCurlDownloadStart(rawURL string) func(error) {
	if !IsGitHubURL(rawURL) {
		return func(error) {}
	}

	started := time.Now()
	log.Printf("github: download url=%s", rawURL)

	return func(err error) {
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			log.Printf("github: download-complete url=%s duration=%s error=%v", rawURL, elapsed, err)
			return
		}
		log.Printf("github: download-complete url=%s duration=%s", rawURL, elapsed)
	}
}
