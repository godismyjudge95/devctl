package api

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/services"
)

func TestProjectConfigFor_NoFileIsNil(t *testing.T) {
	if v := projectConfigFor(t.TempDir(), nil); v != nil {
		t.Errorf("view = %+v, want nil", v)
	}
}

func TestProjectConfigFor_ManagedFieldsAndServiceStatus(t *testing.T) {
	dir := t.TempDir()
	cfg := "php: \"8.4\"\nhttps: false\nservices: [mysql, redis, clickhouse]\n"
	if err := os.WriteFile(filepath.Join(dir, ".devctl.yaml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	states := map[string]services.ServiceState{
		"mysql": {ID: "mysql", Label: "MySQL", Installed: true, Status: services.StatusRunning},
		"redis": {ID: "redis", Label: "Valkey", Installed: true, Status: services.StatusStopped},
	}
	v := projectConfigFor(dir, states)
	if v == nil {
		t.Fatal("view is nil")
	}
	if !slices.Equal(v.Managed, []string{"php_version", "https", "services"}) {
		t.Errorf("managed = %v", v.Managed)
	}
	want := []requiredService{
		{ID: "mysql", Label: "MySQL", Status: "running"},
		{ID: "redis", Label: "Valkey", Status: "stopped"},
		{ID: "clickhouse", Label: "clickhouse", Status: "missing"},
	}
	if !slices.Equal(v.Services, want) {
		t.Errorf("services = %+v, want %+v", v.Services, want)
	}
}

func TestProjectConfigFor_InvalidFileReportsErrorAndManagesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".devctl.local.yaml"), []byte("https: maybe\n"), 0644); err != nil {
		t.Fatal(err)
	}
	v := projectConfigFor(dir, nil)
	if v == nil || !strings.HasPrefix(v.Error, ".devctl.local.yaml: line 1:") {
		t.Fatalf("view = %+v", v)
	}
	if len(v.Managed) != 0 || len(v.Services) != 0 || !slices.Equal(v.Files, []string{".devctl.local.yaml"}) {
		t.Errorf("view = %+v", v)
	}
}
