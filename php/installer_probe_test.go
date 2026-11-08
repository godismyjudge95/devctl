package php

import (
	"context"
	"os"
	"os/exec"
	"os/user"
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

func TestComposerCmd_FindsDevctlPHPWithoutSystemPHP(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "php"), []byte("#!/bin/sh\necho \"devctl php ran $*\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	composer := filepath.Join(bin, "composer")
	if err := os.WriteFile(composer, []byte("#!/usr/bin/env php\n<?php\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", composerCmd(composer, "global config bin-dir --absolute"))
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "devctl php ran "+composer+" global config bin-dir --absolute") {
		t.Fatalf("composer did not run under devctl php: %v\n%s", err, out)
	}
}

func TestLinkComposerGlobalBin_SetsBinDirOnceAndRelinks(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	state := filepath.Join(t.TempDir(), "bin-dir")
	calls := filepath.Join(t.TempDir(), "calls")
	if err := os.WriteFile(state, []byte("/home/u/.config/composer/vendor/bin"), 0o644); err != nil {
		t.Fatal(err)
	}
	composer := filepath.Join(bin, "composer")
	script := "#!/bin/sh\necho \"$*\" >> " + calls + "\n" +
		"echo 'Changed current directory to /home/u/.config/composer' >&2\n" +
		"case \"$*\" in\n" +
		"  'global config bin-dir --absolute') cat " + state + " ;;\n" +
		"  'global config bin-dir '*) printf %s \"$4\" > " + state + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(composer, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := LinkComposerGlobalBin(context.Background(), composer, u.Username, u.HomeDir); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(calls)
	want := "global config bin-dir --absolute\n" +
		"global config bin-dir " + bin + "\n" +
		"global install --no-interaction --quiet\n" +
		"global config bin-dir --absolute\n"
	if string(got) != want {
		t.Fatalf("composer calls:\n%s\nwant:\n%s", got, want)
	}
}
