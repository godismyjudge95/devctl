package php

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgormly/devctl/dist"
)

var staticPHPIndexURL = "https://dl.static-php.dev/static-php-cli/common/"

var staticPHPAssetRe = regexp.MustCompile(`php-(\d+\.\d+\.\d+)-(cli|fpm)-([A-Za-z0-9._-]+)\.tar\.gz`)

func useStaticPHP() bool {
	if os.Getenv("DEVCTL_PHP_RELEASES_API_BASE") != "" {
		return false
	}
	a, err := dist.For("php")
	return err == nil && a.Token != "linux-x86_64"
}

func staticPHPAssetURLs(ctx context.Context, minor string, a dist.Asset) (cliURL, fpmURL string, manifest *ReleaseManifest, err error) {
	cliName, fpmName, ver, err := latestStaticPHPNames(ctx, minor, a.Token)
	if err != nil {
		return "", "", nil, err
	}
	base := strings.TrimRight(staticPHPIndexURL, "/") + "/"
	manifest = &ReleaseManifest{
		ReleaseTag:  "static-php-" + ver,
		PHPVersions: map[string]string{minor: ver},
		Assets: map[string]ReleaseAssets{
			minor: {CLI: cliName, FPM: fpmName},
		},
	}
	return base + cliName, base + fpmName, manifest, nil
}

func latestStaticPHPNames(ctx context.Context, minor, token string) (cliName, fpmName, version string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", staticPHPIndexURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "devctl/1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("static-php index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("static-php index: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}
	type hit struct {
		ver, kind, name string
	}
	var hits []hit
	for _, m := range staticPHPAssetRe.FindAllStringSubmatch(string(body), -1) {
		ver, kind, tok := m[1], m[2], m[3]
		if !strings.HasPrefix(ver, minor+".") || tok != token {
			continue
		}
		hits = append(hits, hit{ver: ver, kind: kind, name: m[0]})
	}
	sort.Slice(hits, func(i, j int) bool { return phpVerLess(hits[i].ver, hits[j].ver) })
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].kind != "cli" {
			continue
		}
		fpm := "php-" + hits[i].ver + "-fpm-" + token + ".tar.gz"
		for _, h := range hits {
			if h.name == fpm {
				return hits[i].name, fpm, hits[i].ver, nil
			}
		}
	}
	return "", "", "", fmt.Errorf("static-php: no cli+fpm pair for %s/%s", minor, token)
}

func phpVerLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			ai, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bi, _ = strconv.Atoi(bs[i])
		}
		if ai != bi {
			return ai < bi
		}
	}
	return false
}
