package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgormly/devctl/doctor"
)

func TestDoctor_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "doctor"}) }
func TestDoctorFix_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "doctor:fix", wantArgs: true})
}

func TestDoctorReport_PrependsDaemonCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/doctor" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"checks":[{"id":"caddy","title":"Caddy running","severity":"fail","message":"down","fixable":true}]}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	report := c.DoctorReport()
	if len(report.Checks) != 2 {
		t.Fatalf("checks = %+v, want daemon + caddy", report.Checks)
	}
	if report.Checks[0].ID != "daemon" || report.Checks[0].Severity != doctor.OK {
		t.Errorf("first check = %+v, want daemon ok", report.Checks[0])
	}
	if report.Checks[1].ID != "caddy" || !report.Checks[1].Fixable {
		t.Errorf("second check = %+v, want the daemon's caddy result", report.Checks[1])
	}
	if err := failedErr(report); err == nil || err.Error() != "1 check failed" {
		t.Errorf("failedErr = %v, want 1 check failed", err)
	}
}

func TestDoctorReport_DaemonDownIsAFailedCheck(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := &Client{base: srv.URL, http: srv.Client()}
	srv.Close()

	report := c.DoctorReport()
	if len(report.Checks) != 1 {
		t.Fatalf("checks = %+v, want only the daemon check", report.Checks)
	}
	d := report.Checks[0]
	if d.ID != "daemon" || d.Severity != doctor.Fail || d.Remedy == "" {
		t.Errorf("daemon check = %+v, want fail with a remedy", d)
	}
	if failedErr(report) == nil {
		t.Error("an unreachable daemon must make doctor exit non-zero")
	}
}

func TestFailedErr_NilWhenOnlyWarnings(t *testing.T) {
	r := doctor.Report{Checks: []doctor.Result{{Severity: doctor.OK}, {Severity: doctor.Warn}}}
	if err := failedErr(r); err != nil {
		t.Errorf("failedErr = %v, want nil for warnings", err)
	}
}

func TestFixDoctor_SendsSelectedIDs(t *testing.T) {
	var got struct {
		IDs []string `json:"ids"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/doctor/fix" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"fixes":[{"id":"php-fpm"}],"checks":[{"id":"php-fpm","title":"PHP-FPM pools running","severity":"ok","message":"1 of 1 running","fixable":false}]}`))
	}))
	defer srv.Close()

	c := &Client{base: srv.URL, http: srv.Client()}
	fr, err := c.FixDoctor([]string{"php-fpm"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IDs) != 1 || got.IDs[0] != "php-fpm" {
		t.Errorf("sent ids = %v, want [php-fpm]", got.IDs)
	}
	if len(fr.Fixes) != 1 || fr.Fixes[0].ID != "php-fpm" || fr.Fixes[0].Error != "" {
		t.Errorf("fixes = %+v", fr.Fixes)
	}
	if len(fr.Checks) != 1 || fr.Checks[0].Severity != doctor.OK {
		t.Errorf("checks = %+v", fr.Checks)
	}
}
