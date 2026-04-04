package api

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// s3Credentials holds AWS-compatible access/secret key credentials for signing.
type s3Credentials struct {
	accessKey string
	secretKey string
	s3Host    string // e.g. "https://s3.maxio.test" or "http://127.0.0.1:9000"
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
func sigV4Sign(req *http.Request, creds s3Credentials, region, service string, bodyBytes []byte) {
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

// presignV4 generates a presigned GET URL for the given object path using
// AWS Signature V4 query-string signing. expires is the number of seconds
// the URL is valid (e.g. "3600"). Returns the full presigned URL string.
func presignV4(creds s3Credentials, region, service, objectPath, defaultPort string, expiresSeconds string) string {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	credential := creds.accessKey + "/" + credentialScope

	// Derive host and scheme from creds.s3Host.
	s3URL, err := url.Parse(creds.s3Host)
	if err != nil || s3URL.Host == "" {
		s3URL = &url.URL{Scheme: "http", Host: "127.0.0.1:" + defaultPort}
	}
	host := s3URL.Host
	scheme := s3URL.Scheme

	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", credential)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", expiresSeconds)
	q.Set("X-Amz-SignedHeaders", "host")

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
				[]byte(region),
			),
			[]byte(service),
		),
		[]byte("aws4_request"),
	)

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	return fmt.Sprintf(
		"%s://%s%s?%s&X-Amz-Signature=%s",
		scheme, host, objectPath, canonicalQueryString, signature,
	)
}

// md5Base64 returns the base64-encoded MD5 of data (for Content-MD5 header).
func md5Base64(data []byte) string {
	h := md5.Sum(data)
	return base64.StdEncoding.EncodeToString(h[:])
}
