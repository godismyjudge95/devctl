package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/danielgormly/devctl/doctor"
	"github.com/danielgormly/devctl/elevate"
)

func init() {
	Register(&Cmd{
		Name:        "doctor",
		Description: "Check the devctl environment; exits 1 when any check fails",
		Examples:    []string{"devctl doctor", "devctl doctor --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			report := c.DoctorReport()
			if jsonMode {
				PrintJSON(report)
			} else {
				printDoctorReport(report)
			}
			return failedErr(report)
		},
	})
	Register(&Cmd{
		Name:        "doctor:fix",
		Description: "Apply the safe, unprivileged fixes doctor offers, then re-check",
		Usage:       "[check-id...]",
		Args: []ArgDef{{
			Name:        "check-id",
			Description: "Only fix these checks (IDs from devctl doctor --json). Default: every fixable check",
			Optional:    true,
		}},
		Examples: []string{"devctl doctor:fix", "devctl doctor:fix php-fpm php-ca-bundle", "devctl doctor:fix --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			fr, err := c.FixDoctor(args)
			if err != nil {
				var ue *url.Error
				if !errors.As(err, &ue) {
					return err
				}
				fr.Fixes = []doctor.FixResult{}
			}
			fr.Checks = append([]doctor.Result{daemonResult(c, err)}, fr.Checks...)
			if jsonMode {
				PrintJSON(fr)
				return failedErr(fr.Report)
			}
			if len(fr.Fixes) == 0 {
				fmt.Println(styleDim.Render("No safe fixes to apply."))
			}
			for _, f := range fr.Fixes {
				if f.Error != "" {
					fmt.Println(styleErr.Render("✗ "+f.ID) + "  " + f.Error)
				} else {
					PrintOK("fixed " + f.ID)
				}
			}
			fmt.Println()
			printDoctorReport(fr.Report)
			return failedErr(fr.Report)
		},
	})
}

// DoctorReport fetches the daemon's report and prepends the one check only a
// client can make: whether the daemon answers at all.
func (c *Client) DoctorReport() doctor.Report {
	var report doctor.Report
	if err := c.get("/api/doctor", &report); err != nil {
		return doctor.Report{Checks: []doctor.Result{daemonResult(c, err)}}
	}
	report.Checks = append([]doctor.Result{daemonResult(c, nil)}, report.Checks...)
	return report
}

// FixDoctor runs POST /api/doctor/fix. Fixes can start services and wait for
// them, so it uses a longer timeout than other calls.
func (c *Client) FixDoctor(ids []string) (doctor.FixReport, error) {
	var fr doctor.FixReport
	body := map[string][]string{"ids": ids}
	err := c.postTimeout("/api/doctor/fix", body, &fr, 2*time.Minute)
	return fr, err
}

func daemonResult(c *Client, err error) doctor.Result {
	r := doctor.Result{ID: "daemon", Title: "Daemon reachable"}
	if err == nil {
		r.Severity = doctor.OK
		r.Message = "Answers at " + c.base
		return r
	}
	r.Severity = doctor.Fail
	r.Message = err.Error()
	var ue *url.Error
	if !errors.As(err, &ue) {
		r.Message = "The daemon answered with an error (is it older than this CLI?): " + err.Error()
		r.Remedy = "Restart the daemon so it runs this devctl version"
		return r
	}
	r.Message = "Nothing answers at " + c.base
	if runtime.GOOS == "darwin" {
		r.Remedy = fmt.Sprintf("launchctl kickstart -k gui/%d/%s", os.Getuid(), elevate.LaunchAgentLabel)
	} else {
		r.Remedy = "sudo systemctl restart devctl"
	}
	return r
}

func failedErr(r doctor.Report) error {
	n := r.Failed()
	if n == 0 {
		return nil
	}
	if n == 1 {
		return errors.New("1 check failed")
	}
	return fmt.Errorf("%d checks failed", n)
}

func printDoctorReport(r doctor.Report) {
	Header("devctl doctor")
	counts := map[doctor.Severity]int{}
	for _, c := range r.Checks {
		counts[c.Severity]++
		var mark string
		switch c.Severity {
		case doctor.OK:
			mark = styleOK.Render("✓")
		case doctor.Warn:
			mark = styleWarn.Render("!")
		default:
			mark = styleErr.Render("✗")
		}
		fmt.Printf("  %s %-32s %s\n", mark, c.Title, styleDim.Render(c.Message))
		for _, d := range c.Details {
			fmt.Printf("      %s\n", d)
		}
		if c.Severity == doctor.OK {
			continue
		}
		if c.Remedy != "" {
			fmt.Printf("      %s %s\n", styleLabel.Render("fix:"), c.Remedy)
		}
		if c.Fixable {
			fmt.Printf("      %s devctl doctor:fix %s\n", styleLabel.Render("auto:"), c.ID)
		}
	}
	fmt.Println()
	summary := []string{
		fmt.Sprintf("%d ok", counts[doctor.OK]),
		fmt.Sprintf("%d warnings", counts[doctor.Warn]),
		fmt.Sprintf("%d failed", counts[doctor.Fail]),
	}
	fmt.Println(strings.Join(summary, ", "))
}
