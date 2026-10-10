package php

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func TestWriteCLIWrapper_ExportsCAAndPassesIni(t *testing.T) {
	serverRoot := t.TempDir()
	ver := "8.4"
	phpDir := PHPDir(ver, serverRoot)
	if err := os.MkdirAll(phpDir, 0755); err != nil {
		t.Fatal(err)
	}
	cliBin := filepath.Join(phpDir, "php")
	if err := os.WriteFile(cliBin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(paths.BinDir(serverRoot), "php8.4")
	if err := writeCLIWrapper(wrapper, cliBin, serverRoot); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	bundle := paths.CABundlePath(serverRoot)
	ini := filepath.Join(phpDir, "php.ini")
	for _, want := range []string{
		"export SSL_CERT_FILE=" + strconv.Quote(bundle),
		"export CURL_CA_BUNDLE=" + strconv.Quote(bundle),
		"export AWS_CA_BUNDLE=" + strconv.Quote(bundle),
		"exec " + strconv.Quote(cliBin) + " -c " + strconv.Quote(ini),
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("wrapper missing %q\n%s", want, s)
		}
	}
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("wrapper not executable: %v", info.Mode())
	}
}
