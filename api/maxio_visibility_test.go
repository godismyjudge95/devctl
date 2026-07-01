package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateMaxioBucketName(t *testing.T) {
	valid := []string{"my-bucket", "prod-logs.2026", "infomedia-tools"}
	for _, name := range valid {
		if err := validateMaxioBucketName(name); err != nil {
			t.Errorf("%q should be valid: %v", name, err)
		}
	}
	invalid := []string{"", "ab", "../evil", "a/b", "UPPER", "a.-b", ".hidden", "bucket..name"}
	for _, name := range invalid {
		if err := validateMaxioBucketName(name); err == nil {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestReadWriteMaxioBucketVisibility(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, ".bucket.json")
	initial := `{
  "name": "test-bucket",
  "created_at": "2026-01-01T00:00:00.000Z",
  "region": "us-east-1",
  "versioning": false,
  "public_read": false,
  "public_list": false
}`
	if err := os.WriteFile(metaPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	vis, err := readMaxioBucketVisibility(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if vis.PublicRead || vis.PublicList {
		t.Fatalf("expected private bucket, got %+v", vis)
	}

	updated, err := writeMaxioBucketVisibility(metaPath, maxioBucketVisibility{
		PublicRead: true,
		PublicList: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.PublicRead || updated.PublicList {
		t.Fatalf("unexpected updated visibility: %+v", updated)
	}

	vis, err = readMaxioBucketVisibility(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !vis.PublicRead || vis.PublicList {
		t.Fatalf("expected public_read only, got %+v", vis)
	}
}

func TestReadMaxioBucketVisibilityNotFound(t *testing.T) {
	_, err := readMaxioBucketVisibility(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || err.Error() != "bucket not found" {
		t.Fatalf("expected bucket not found, got %v", err)
	}
}