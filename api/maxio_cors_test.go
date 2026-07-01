package api

import (
	"net/http/httptest"
	"testing"
)

func TestMaxioBucketPutName(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
		ok     bool
	}{
		{"PUT", "/api/maxio/s3/my-bucket", "my-bucket", true},
		{"PUT", "/api/maxio/s3/infomedia-tools", "infomedia-tools", true},
		{"PUT", "/api/maxio/s3/my-bucket/object.png", "", false},
		{"GET", "/api/maxio/s3/my-bucket", "", false},
		{"PUT", "/api/maxio/s3/my-bucket?cors", "", false},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		got, ok := maxioBucketPutName(req)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s %s: got (%q, %v), want (%q, %v)", tc.method, tc.path, got, ok, tc.want, tc.ok)
		}
	}
}