package php

import "testing"

func TestParsePatchVersion(t *testing.T) {
	got, err := ParsePatchVersion("PHP 8.4.19 (cli) (built: Apr 22 2026 21:07:11)\nCopyright")
	if err != nil {
		t.Fatalf("ParsePatchVersion: %v", err)
	}
	if got != "8.4.19" {
		t.Fatalf("got %q, want %q", got, "8.4.19")
	}
}

func TestParsePatchVersion_Invalid(t *testing.T) {
	if _, err := ParsePatchVersion("not php output"); err == nil {
		t.Fatal("expected parse error")
	}
}
