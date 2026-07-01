//go:build integration

package apitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type maxioVisibility struct {
	PublicRead bool `json:"publicRead"`
	PublicList bool `json:"publicList"`
}

func requireMaxioInstalled(t *testing.T) {
	t.Helper()
	body := httpGet(t, "/api/services")
	var services []struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(body, &services); err != nil {
		t.Fatalf("parse services: %v", err)
	}
	for _, svc := range services {
		if svc.ID == "maxio" && svc.Installed {
			return
		}
	}
	t.Skip("maxio not installed — skipping visibility test")
}

func TestMaxIOBucketVisibility(t *testing.T) {
	requireMaxioInstalled(t)

	bucket := fmt.Sprintf("vis-test-%d", time.Now().UnixNano())
	createPath := fmt.Sprintf("/api/maxio/s3/%s", bucket)
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s%s", baseURL(), createPath), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("create bucket: status %d", resp.StatusCode)
	}
	t.Cleanup(func() {
		delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/maxio/s3/%s", baseURL(), bucket), nil)
		if delResp, err := http.DefaultClient.Do(delReq); err == nil {
			delResp.Body.Close()
		}
	})

	visPath := fmt.Sprintf("/api/maxio/buckets/%s/visibility", bucket)
	body := httpGet(t, visPath)
	var vis maxioVisibility
	if err := json.Unmarshal(body, &vis); err != nil {
		t.Fatalf("parse visibility: %v", err)
	}
	if vis.PublicRead || vis.PublicList {
		t.Fatalf("new bucket should be private, got %+v", vis)
	}

	putBody, status := httpPut(t, visPath, maxioVisibility{PublicRead: true, PublicList: false})
	if status != http.StatusOK {
		t.Fatalf("PUT visibility: status %d body %s", status, putBody)
	}
	if err := json.Unmarshal(putBody, &vis); err != nil {
		t.Fatalf("parse PUT response: %v", err)
	}
	if !vis.PublicRead || vis.PublicList {
		t.Fatalf("expected public_read=true, got %+v", vis)
	}

	body = httpGet(t, visPath)
	if err := json.Unmarshal(body, &vis); err != nil {
		t.Fatalf("parse visibility after PUT: %v", err)
	}
	if !vis.PublicRead {
		t.Fatalf("GET after PUT should be public, got %+v", vis)
	}
}