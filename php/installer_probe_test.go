package php

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "php")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeBinary_AcceptsWorkingPHP(t *testing.T) {
	bin := writeScript(t, `echo "PHP 8.4.1 (cli)"`)
	if err := probeBinary(context.Background(), bin); err != nil {
		t.Fatalf("probe: %v", err)
	}
}

func TestProbeBinary_ExplainsOldGlibc(t *testing.T) {
	bin := writeScript(t, `echo "$0: /lib/x86_64-linux-gnu/libc.so.6: version 'GLIBC_2.38' not found (required by $0)" >&2; exit 1`)
	err := probeBinary(context.Background(), bin)
	if err == nil {
		t.Fatal("probe accepted a binary that does not start")
	}
	for _, want := range []string{"glibc is too old", "Ubuntu 24.04", "did not change", "GLIBC_2.38"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestProbeBinary_ReportsOtherStartFailures(t *testing.T) {
	bin := writeScript(t, `echo "Segmentation fault" >&2; exit 139`)
	err := probeBinary(context.Background(), bin)
	if err == nil || strings.Contains(err.Error(), "glibc") || !strings.Contains(err.Error(), "Segmentation fault") {
		t.Fatalf("unexpected error: %v", err)
	}
}
