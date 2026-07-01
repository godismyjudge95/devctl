package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxioDevCORSXML is applied to every bucket so browser clients (e.g. Laravel
// Livewire direct-to-S3 uploads) can PUT/GET against s3.maxio.test from *.test sites.
const maxioDevCORSXML = `<?xml version="1.0" encoding="UTF-8"?>
<CORSConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <CORSRule>
    <AllowedOrigin>*</AllowedOrigin>
    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedMethod>POST</AllowedMethod>
    <AllowedMethod>DELETE</AllowedMethod>
    <AllowedMethod>HEAD</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <ExposeHeader>ETag</ExposeHeader>
    <ExposeHeader>x-amz-request-id</ExposeHeader>
    <MaxAgeSeconds>3600</MaxAgeSeconds>
  </CORSRule>
</CORSConfiguration>`

// maxioBucketPutName reports whether r is a bucket-level PUT (create bucket).
func maxioBucketPutName(r *http.Request) (string, bool) {
	if r.Method != http.MethodPut {
		return "", false
	}
	stripped := strings.TrimPrefix(r.URL.Path, "/api/maxio/s3")
	stripped = strings.Trim(stripped, "/")
	if stripped == "" || strings.Contains(stripped, "/") {
		return "", false
	}
	q := r.URL.Query()
	if q.Has("cors") || q.Has("delete") || q.Has("location") || q.Has("tagging") ||
		q.Has("lifecycle") || q.Has("versioning") || q.Has("encryption") {
		return "", false
	}
	return stripped, true
}

func (s *Server) maxioSignedRequest(ctx context.Context, method, path string, query string, body []byte) (*http.Response, error) {
	creds, err := s.maxioReadCredentials()
	if err != nil {
		return nil, err
	}
	target := "http://127.0.0.1:" + maxioS3Port + path
	if query != "" {
		target += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/xml")
	}
	req.Host = "127.0.0.1:" + maxioS3Port
	sigV4Sign(req, creds, maxioRegion, maxioService, body)
	return http.DefaultClient.Do(req)
}

func (s *Server) maxioListBucketNames(ctx context.Context) ([]string, error) {
	resp, err := s.maxioSignedRequest(ctx, http.MethodGet, "/", "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list buckets: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var result struct {
		Buckets struct {
			Bucket []struct {
				Name string `xml:"Name"`
			} `xml:"Bucket"`
		} `xml:"Buckets"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list buckets: decode: %w", err)
	}
	names := make([]string, 0, len(result.Buckets.Bucket))
	for _, b := range result.Buckets.Bucket {
		if b.Name != "" {
			names = append(names, b.Name)
		}
	}
	return names, nil
}

func (s *Server) putBucketCORS(ctx context.Context, bucket string) error {
	body := []byte(maxioDevCORSXML)
	resp, err := s.maxioSignedRequest(ctx, http.MethodPut, "/"+bucket, "cors", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("put bucket CORS %q: HTTP %d: %s", bucket, resp.StatusCode, strings.TrimSpace(string(b)))
}

// syncMaxioBucketCORS applies the dev CORS policy to every existing bucket.
func (s *Server) syncMaxioBucketCORS(ctx context.Context) (int, error) {
	names, err := s.maxioListBucketNames(ctx)
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		if err := s.putBucketCORS(ctx, name); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}

func (s *Server) handleMaxIOCORSSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	n, err := s.syncMaxioBucketCORS(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"buckets": n, "status": "ok"})
}