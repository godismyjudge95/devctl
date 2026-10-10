package sites

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCAChain_ConcatenatesRootAndIntermediate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pki/ca/local" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"root_certificate":"-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n","intermediate_certificate":"-----BEGIN CERTIFICATE-----\nINTER\n-----END CERTIFICATE-----"}`))
	}))
	defer srv.Close()

	got, err := NewCaddyClient(srv.URL).CAChain()
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "ROOT") || !strings.Contains(s, "INTER") {
		t.Fatalf("chain missing certs:\n%s", s)
	}
}

func TestRootCert_ReturnsOnlyRoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"root_certificate":"-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n","intermediate_certificate":"-----BEGIN CERTIFICATE-----\nINTER\n-----END CERTIFICATE-----"}`))
	}))
	defer srv.Close()

	got, err := NewCaddyClient(srv.URL).RootCert()
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "ROOT") {
		t.Fatalf("missing root:\n%s", s)
	}
	if strings.Contains(s, "INTER") {
		t.Fatalf("RootCert must not include intermediate (elevate trust needs one cert):\n%s", s)
	}
}
