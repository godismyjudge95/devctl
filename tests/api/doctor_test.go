//go:build integration

package apitest

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type doctorResult struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Details  []string `json:"details"`
	Remedy   string   `json:"remedy"`
	Fixable  bool     `json:"fixable"`
}

type doctorReport struct {
	Checks []doctorResult `json:"checks"`
}

type doctorFixReport struct {
	Fixes []struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	} `json:"fixes"`
	Checks []doctorResult `json:"checks"`
}

func getDoctor(t *testing.T) map[string]doctorResult {
	t.Helper()
	report := decodeJSON[doctorReport](t, httpGet(t, "/api/doctor"))
	return byID(report.Checks)
}

func byID(checks []doctorResult) map[string]doctorResult {
	out := make(map[string]doctorResult, len(checks))
	for _, c := range checks {
		out[c.ID] = c
	}
	return out
}

func doctorServerRoot() string {
	if r := os.Getenv("DEVCTL_SERVER_ROOT"); r != "" {
		return r
	}
	return "/home/testuser/ddev/sites/server"
}

// createDoctorSite registers a site whose PHP version is not installed and
// returns its root directory.
func createDoctorSite(t *testing.T, domain string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "devctl-doctor-site-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, s := range decodeJSON[[]Site](t, httpGet(t, "/api/sites")) {
		if s.Domain == domain {
			httpDelete(t, "/api/sites/"+s.ID)
		}
	}
	body, status := httpPost(t, "/api/sites", map[string]any{
		"domain":      domain,
		"root_path":   dir,
		"php_version": "5.6",
	})
	if status != http.StatusCreated {
		t.Fatalf("create site: status %d: %s", status, body)
	}
	site := decodeJSON[Site](t, body)
	t.Cleanup(func() { httpDelete(t, "/api/sites/"+site.ID) })
	return dir
}

func detailsMention(c doctorResult, s string) bool {
	for _, d := range c.Details {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}

func TestDoctor_ReportListsEveryCheck(t *testing.T) {
	report := decodeJSON[doctorReport](t, httpGet(t, "/api/doctor"))
	want := []string{
		"elevate-daemon", "caddy", "port-http", "port-https", "dns-server", "resolver",
		"ca-trust", "php-ca-bundle", "php-installed", "php-fpm", "site-php", "site-roots",
		"shell-path", "service-configs", "devctl-update", "disk-space",
	}
	if len(report.Checks) != len(want) {
		t.Fatalf("got %d checks, want %d: %+v", len(report.Checks), len(want), report.Checks)
	}
	for i, c := range report.Checks {
		if c.ID != want[i] {
			t.Errorf("check %d: id %q, want %q", i, c.ID, want[i])
		}
		if c.Title == "" || c.Message == "" {
			t.Errorf("%s: empty title or message: %+v", c.ID, c)
		}
		switch c.Severity {
		case "ok":
			if c.Fixable {
				t.Errorf("%s: ok check must not be fixable", c.ID)
			}
		case "warn", "fail":
		default:
			t.Errorf("%s: severity %q", c.ID, c.Severity)
		}
	}
	checks := byID(report.Checks)
	if checks["php-installed"].Severity != "ok" {
		t.Errorf("php-installed: want ok with the PHP 8.4 stub, got %+v", checks["php-installed"])
	}
	if checks["elevate-daemon"].Severity != "ok" {
		t.Errorf("elevate-daemon: want ok in the test env, got %+v", checks["elevate-daemon"])
	}
}

func TestDoctor_FlagsSiteWithMissingPHPAndRoot(t *testing.T) {
	const domain = "doctor-broken.test"
	dir := createDoctorSite(t, domain)

	checks := getDoctor(t)
	if c := checks["site-php"]; c.Severity != "fail" || !detailsMention(c, domain) {
		t.Errorf("site-php: want fail naming %s, got %+v", domain, c)
	}
	if c := checks["site-roots"]; detailsMention(c, domain) {
		t.Errorf("site-roots: %s still exists but is reported: %+v", domain, c)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	checks = getDoctor(t)
	if c := checks["site-roots"]; c.Severity != "fail" || !detailsMention(c, domain) || c.Fixable {
		t.Errorf("site-roots: want unfixable fail naming %s, got %+v", domain, c)
	}
}

func TestDoctorFix_RebuildsPHPCABundle(t *testing.T) {
	bundle := filepath.Join(doctorServerRoot(), "php", "ca-bundle.crt")
	if err := os.Remove(bundle); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove bundle: %v", err)
	}

	if c := getDoctor(t)["php-ca-bundle"]; c.Severity != "warn" || !c.Fixable {
		t.Fatalf("php-ca-bundle with no bundle: want fixable warn, got %+v", c)
	}

	body, status := httpPost(t, "/api/doctor/fix", map[string]any{"ids": []string{"php-ca-bundle"}})
	if status != http.StatusOK {
		t.Fatalf("fix: status %d: %s", status, body)
	}
	fr := decodeJSON[doctorFixReport](t, body)
	if len(fr.Fixes) != 1 || fr.Fixes[0].ID != "php-ca-bundle" || fr.Fixes[0].Error != "" {
		t.Fatalf("fixes = %+v, want one clean php-ca-bundle fix", fr.Fixes)
	}
	if _, err := os.Stat(bundle); err != nil {
		t.Errorf("bundle not rebuilt: %v", err)
	}
	if c := byID(fr.Checks)["php-ca-bundle"]; c.Severity != "ok" {
		t.Errorf("php-ca-bundle after fix: want ok, got %+v", c)
	}
}

func TestDoctorFix_RejectsUnknownCheck(t *testing.T) {
	body, status := httpPost(t, "/api/doctor/fix", map[string]any{"ids": []string{"no-such-check"}})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", status, body)
	}
}

func TestDoctorCLI_ExitsNonZeroOnFailure(t *testing.T) {
	createDoctorSite(t, "doctor-cli.test")

	cmd := exec.Command("devctl", "doctor", "--json")
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("devctl doctor: want exit 1 with a failing site, got err=%v\n%s", err, out)
	}
	var report doctorReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("stdout is not a JSON report: %v\n%s", err, out)
	}
	if len(report.Checks) == 0 || report.Checks[0].ID != "daemon" || report.Checks[0].Severity != "ok" {
		t.Errorf("first check: want client-side daemon ok, got %+v", report.Checks)
	}
	if c := byID(report.Checks)["site-php"]; !detailsMention(c, "doctor-cli.test") {
		t.Errorf("site-php: want doctor-cli.test in details, got %+v", c)
	}
}
