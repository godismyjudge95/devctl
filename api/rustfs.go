package api

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielgormly/devctl/paths"
)

const (
	rustfsS3Port    = "9000"
	rustfsAdminPort = "9001"
	rustfsRegion    = "us-east-1"
	rustfsService   = "s3"
)

// rustfsCredentials holds the parsed access/secret key from config.env.
type rustfsCredentials struct {
	accessKey string
	secretKey string
	s3Host    string // e.g. "https://s3.rustfs.test" — falls back to "http://127.0.0.1:9000"
}

// rustfsReadCredentials parses RUSTFS_ACCESS_KEY and RUSTFS_SECRET_KEY from
// {serverRoot}/rustfs/config.env.
func (s *Server) rustfsReadCredentials() (rustfsCredentials, error) {
	envPath := filepath.Join(paths.ServiceDir(s.serverRoot, "rustfs"), "config.env")
	f, err := os.Open(envPath)
	if err != nil {
		return rustfsCredentials{}, fmt.Errorf("rustfs: open config.env: %w", err)
	}
	defer f.Close()

	creds := rustfsCredentials{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "RUSTFS_ACCESS_KEY=") {
			creds.accessKey = strings.TrimPrefix(line, "RUSTFS_ACCESS_KEY=")
		} else if strings.HasPrefix(line, "RUSTFS_SECRET_KEY=") {
			creds.secretKey = strings.TrimPrefix(line, "RUSTFS_SECRET_KEY=")
		} else if strings.HasPrefix(line, "RUSTFS_S3_HOST=") {
			creds.s3Host = strings.TrimPrefix(line, "RUSTFS_S3_HOST=")
		}
	}
	if err := scanner.Err(); err != nil {
		return rustfsCredentials{}, fmt.Errorf("rustfs: read config.env: %w", err)
	}
	if creds.accessKey == "" || creds.secretKey == "" {
		return rustfsCredentials{}, fmt.Errorf("rustfs: missing credentials in config.env")
	}
	if creds.s3Host == "" {
		creds.s3Host = "http://127.0.0.1:" + rustfsS3Port
	}
	return creds, nil
}

// ── AWS Signature V4 helpers ──────────────────────────────────────────────────

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// sigV4Sign adds AWS Signature V4 Authorization + x-amz-date headers to req.
// bodyBytes is the raw request body (may be nil/empty for GET/DELETE).
func sigV4Sign(req *http.Request, creds rustfsCredentials, region, service string, bodyBytes []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)

	// Payload hash.
	payloadHash := sha256Hex(bodyBytes)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	host := req.URL.Host
	if host == "" {
		host = req.Host
	}

	// Build signed headers map — always include host, x-amz-content-sha256, x-amz-date.
	// Also include Content-MD5 and Content-Type if present (required for POST ?delete).
	headersToSign := map[string]string{
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	if v := req.Header.Get("Content-Md5"); v != "" {
		headersToSign["content-md5"] = v
	}
	if v := req.Header.Get("Content-Type"); v != "" {
		headersToSign["content-type"] = v
	}
	// Include any x-amz-* headers that were copied from the original request.
	for k, vals := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") && lk != "x-amz-date" && lk != "x-amz-content-sha256" {
			headersToSign[lk] = vals[0]
		}
	}

	// Sort header names for canonical form.
	signedHeaderNames := make([]string, 0, len(headersToSign))
	for k := range headersToSign {
		signedHeaderNames = append(signedHeaderNames, k)
	}
	sort.Strings(signedHeaderNames)

	// Build canonical headers string.
	var canonicalHeadersBuf strings.Builder
	for _, k := range signedHeaderNames {
		canonicalHeadersBuf.WriteString(k)
		canonicalHeadersBuf.WriteByte(':')
		canonicalHeadersBuf.WriteString(strings.TrimSpace(headersToSign[k]))
		canonicalHeadersBuf.WriteByte('\n')
	}
	canonicalHeaders := canonicalHeadersBuf.String()
	signedHeadersStr := strings.Join(signedHeaderNames, ";")

	// Canonical query string — per AWS Sig V4 spec, sort by key and ensure
	// each parameter is represented as "key=value" (empty value uses "key=").
	var canonicalQueryString string
	if req.URL.RawQuery != "" {
		queryParts := strings.Split(req.URL.RawQuery, "&")
		normalised := make([]string, 0, len(queryParts))
		for _, part := range queryParts {
			if part == "" {
				continue
			}
			if !strings.Contains(part, "=") {
				// Bare key (e.g. "delete") — AWS requires "delete=".
				part = part + "="
			}
			normalised = append(normalised, part)
		}
		sort.Strings(normalised)
		canonicalQueryString = strings.Join(normalised, "&")
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQueryString,
		canonicalHeaders,
		signedHeadersStr,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Derive signing key.
	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+creds.secretKey), []byte(dateStamp)),
				[]byte(region),
			),
			[]byte(service),
		),
		[]byte("aws4_request"),
	)

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.accessKey, credentialScope, signedHeadersStr, signature,
	)
	req.Header.Set("Authorization", authHeader)
}

// ── Proxy helpers ─────────────────────────────────────────────────────────────

// rustfsProxyRequest reads the body, signs, and forwards to the given base URL.
func (s *Server) rustfsProxyRequest(w http.ResponseWriter, r *http.Request, targetBase, stripPrefix string) {
	creds, err := s.rustfsReadCredentials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read body so we can sign it.
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
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
		return
	}
	target.Path = stripped
	target.RawQuery = r.URL.RawQuery

	// Build upstream request.
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy original headers (Content-Type, Content-MD5, etc.).
	for key, vals := range r.Header {
		k := http.CanonicalHeaderKey(key)
		// Skip hop-by-hop headers.
		if k == "Connection" || k == "Te" || k == "Trailers" || k == "Transfer-Encoding" ||
			k == "Upgrade" || k == "X-Forwarded-For" {
			continue
		}
		upReq.Header[k] = vals
	}
	upReq.Host = target.Host

	// Sign.
	sigV4Sign(upReq, creds, rustfsRegion, rustfsService, bodyBytes)

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
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
}

// handleRustFSS3Proxy proxies /api/rustfs/s3/* → RustFS S3 port (9000).
func (s *Server) handleRustFSS3Proxy(w http.ResponseWriter, r *http.Request) {
	s.rustfsProxyRequest(w, r, "http://127.0.0.1:"+rustfsS3Port, "/api/rustfs/s3")
}

// handleRustFSAdminProxy proxies /api/rustfs/admin/* → RustFS Admin port (9001).
func (s *Server) handleRustFSAdminProxy(w http.ResponseWriter, r *http.Request) {
	s.rustfsProxyRequest(w, r, "http://127.0.0.1:"+rustfsAdminPort, "/api/rustfs/admin")
}

// handleRustFSPresign generates a presigned GET URL for ?bucket=...&key=...
// and returns JSON {"url":"..."}.
func (s *Server) handleRustFSPresign(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	key := r.URL.Query().Get("key")
	if bucket == "" || key == "" {
		http.Error(w, "bucket and key are required", http.StatusBadRequest)
		return
	}

	creds, err := s.rustfsReadCredentials()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	expires := "3600"

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, rustfsRegion, rustfsService)
	credential := creds.accessKey + "/" + credentialScope

	objectPath := "/" + bucket + "/" + strings.TrimPrefix(key, "/")

	// Build query string for presigning.
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", expires)
	q.Set("X-Amz-SignedHeaders", "host")

	// Derive host and scheme from creds.s3Host (e.g. "https://s3.rustfs.test").
	s3URL, err := url.Parse(creds.s3Host)
	if err != nil || s3URL.Host == "" {
		s3URL = &url.URL{Scheme: "http", Host: "127.0.0.1:" + rustfsS3Port}
	}
	host := s3URL.Host
	scheme := s3URL.Scheme

	// Sort query params for canonical request.
	queryParts := strings.Split(q.Encode(), "&")
	sort.Strings(queryParts)
	canonicalQueryString := strings.Join(queryParts, "&")

	canonicalHeaders := "host:" + host + "\n"
	canonicalRequest := strings.Join([]string{
		"GET",
		objectPath,
		canonicalQueryString,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+creds.secretKey), []byte(dateStamp)),
				[]byte(rustfsRegion),
			),
			[]byte(rustfsService),
		),
		[]byte("aws4_request"),
	)

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	presignedURL := fmt.Sprintf(
		"%s://%s%s?%s&X-Amz-Signature=%s",
		scheme, host, objectPath, canonicalQueryString, signature,
	)

	writeJSON(w, map[string]string{"url": presignedURL})
}

// md5Base64 returns the base64-encoded MD5 of data (for Content-MD5 header).
func md5Base64(data []byte) string {
	h := md5.Sum(data)
	return base64.StdEncoding.EncodeToString(h[:])
}
