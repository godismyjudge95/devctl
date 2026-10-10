package config

import (
	"strings"
	"testing"

	"github.com/danielgormly/devctl/services"
)

func TestCaddyStartsWithResume(t *testing.T) {
	var def services.Definition
	for _, d := range DefaultServices("/tmp/server", "alice") {
		if d.ID == "caddy" {
			def = d
			break
		}
	}
	if def.ID == "" {
		t.Fatal("caddy definition missing")
	}

	args, err := services.ParseCommandArgs(def.ManagedArgs)
	if err != nil {
		t.Fatalf("parse ManagedArgs: %v", err)
	}
	if len(args) < 2 || args[0] != "run" || args[1] != "--resume" {
		t.Fatalf("caddy ManagedArgs = %q, want run --resume", def.ManagedArgs)
	}

	if !strings.Contains(def.HealthCheck, "/config/apps/http/servers/devctl") {
		t.Fatalf("caddy HealthCheck = %q, want HTTP server probe", def.HealthCheck)
	}
	if def.HealthCheckRetries < 1 {
		t.Fatalf("caddy HealthCheckRetries = %d, want retries after empty start", def.HealthCheckRetries)
	}
}
