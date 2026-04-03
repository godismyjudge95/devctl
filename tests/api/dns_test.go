//go:build integration

package apitest

import (
	"testing"
)

// DNSSetupStatus mirrors the JSON shape returned by GET /api/dns/setup.
type DNSSetupStatus struct {
	Configured bool   `json:"configured"`
	IP         string `json:"ip"`
}

// TestDNSCheckSetup_StatusOK verifies GET /api/dns/setup returns HTTP 200.
func TestDNSCheckSetup_StatusOK(t *testing.T) {
	httpGet(t, "/api/dns/setup")
}

// TestDNSCheckSetup_Unmarshals verifies the response body can be decoded.
func TestDNSCheckSetup_Unmarshals(t *testing.T) {
	body := httpGet(t, "/api/dns/setup")
	_ = decodeJSON[DNSSetupStatus](t, body)
}

// TestDNSDetectIP_StatusOK verifies GET /api/dns/detect-ip returns HTTP 200.
func TestDNSDetectIP_StatusOK(t *testing.T) {
	httpGet(t, "/api/dns/detect-ip")
}
