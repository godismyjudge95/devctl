package api

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/paths"
)

const (
	// Internal loopback port — clients reach MaxIO via s3.maxio.test / the
	// dashboard proxy, not this address. Kept off 9000 so ClickHouse can use
	// its default native TCP port.
	maxioS3Port  = "9900"
	maxioRegion  = "us-east-1"
	maxioService = "s3"
)

// maxioReadCredentials parses MAXIO_ACCESS_KEY and MAXIO_SECRET_KEY from
// {serverRoot}/maxio/config.env.
func (s *Server) maxioReadCredentials() (s3Credentials, error) {
	envPath := filepath.Join(paths.ServiceDir(s.serverRoot, "maxio"), "config.env")
	f, err := os.Open(envPath)
	if err != nil {
		return s3Credentials{}, fmt.Errorf("maxio: open config.env: %w", err)
	}
	defer f.Close()

	creds := s3Credentials{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "MAXIO_ACCESS_KEY=") {
			creds.accessKey = strings.TrimPrefix(line, "MAXIO_ACCESS_KEY=")
		} else if strings.HasPrefix(line, "MAXIO_SECRET_KEY=") {
			creds.secretKey = strings.TrimPrefix(line, "MAXIO_SECRET_KEY=")
		} else if strings.HasPrefix(line, "MAXIO_S3_HOST=") {
			creds.s3Host = strings.TrimPrefix(line, "MAXIO_S3_HOST=")
		}
	}
	if err := scanner.Err(); err != nil {
		return s3Credentials{}, fmt.Errorf("maxio: read config.env: %w", err)
	}
	if creds.accessKey == "" || creds.secretKey == "" {
		return s3Credentials{}, fmt.Errorf("maxio: missing credentials in config.env")
	}
	if creds.s3Host == "" {
		creds.s3Host = "http://127.0.0.1:" + maxioS3Port
	}
	return creds, nil
}

// ── Proxy helpers ─────────────────────────────────────────────────────────────

// maxioProxyRequest reads the body, signs, and forwards to the given base URL.
// It returns the upstream HTTP status code.
func (s *Server) maxioProxyRequest(w http.ResponseWriter, r *http.Request, targetBase, stripPrefix string) int {
	creds, err := s.maxioReadCredentials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Read body so we can sign it.
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return http.StatusBadRequest
		}
	}

	// Build target URL.
	stripped := strings.TrimPrefix(r.URL.Path, stripPrefix)
	if stripped == "" {
		stripped = "/"
	}
	target, err := url.Parse(targetBase)
	if err != nil {
		http.Error(w, "invalid target: "+err.Error(), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	target.Path = stripped
	target.RawQuery = r.URL.RawQuery

	// Build upstream request.
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Copy original headers (Content-Type, Content-MD5, etc.).
	for key, vals := range r.Header {
		k := http.CanonicalHeaderKey(key)
		// Skip hop-by-hop and browser-origin headers. The proxy talks to MaxIO
		// server-side; forwarding Origin/Referer can trigger MaxIO CORS handling.
		if k == "Connection" || k == "Te" || k == "Trailers" || k == "Transfer-Encoding" ||
			k == "Upgrade" || k == "X-Forwarded-For" || k == "Origin" || k == "Referer" {
			continue
		}
		upReq.Header[k] = vals
	}
	upReq.Host = target.Host

	// Sign.
	sigV4Sign(upReq, creds, maxioRegion, maxioService, bodyBytes)

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer resp.Body.Close()

	// Forward response headers.
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
	return resp.StatusCode
}

// handleMaxIOS3Proxy proxies /api/maxio/s3/* → MaxIO S3 port (9900).
func (s *Server) handleMaxIOS3Proxy(w http.ResponseWriter, r *http.Request) {
	bucket, applyCORS := maxioBucketPutName(r)
	status := s.maxioProxyRequest(w, r, "http://127.0.0.1:"+maxioS3Port, "/api/maxio/s3")
	if applyCORS && (status == http.StatusOK || status == http.StatusConflict) {
		if err := s.putBucketCORS(r.Context(), bucket); err != nil {
			log.Printf("maxio: put bucket CORS %q: %v", bucket, err)
		}
	}
}

// handleMaxIOPresign generates a presigned GET URL for ?bucket=...&key=...
// and returns JSON {"url":"..."}.
func (s *Server) handleMaxIOPresign(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	key := r.URL.Query().Get("key")
	if bucket == "" || key == "" {
		http.Error(w, "bucket and key are required", http.StatusBadRequest)
		return
	}

	creds, err := s.maxioReadCredentials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	objectPath := "/" + bucket + "/" + strings.TrimPrefix(key, "/")
	presignedURL := presignV4(creds, maxioRegion, maxioService, objectPath, maxioS3Port, "3600")
	writeJSON(w, map[string]string{"url": presignedURL})
}
