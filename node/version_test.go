package node

import "testing"

func installedVersions(t *testing.T, list ...string) []Version {
	t.Helper()
	var out []Version
	for _, s := range list {
		v, ok := ParseVersion(s)
		if !ok {
			t.Fatalf("ParseVersion(%q) failed", s)
		}
		out = append(out, v)
	}
	return out
}

func TestResolve(t *testing.T) {
	installed := installedVersions(t,
		"v16.20.2", "v18.16.0", "v18.19.1", "v20.11.1", "v21.7.3", "v22.11.0", "v23.3.0",
	)
	tests := []struct {
		want     string
		resolved string // "" means no installed version matches
	}{
		{"20", "v20.11.1"},
		{"v20", "v20.11.1"},
		{"18.16", "v18.16.0"},
		{"v18.19.1", "v18.19.1"},
		{"18.19.0", ""},
		{"20.x", "v20.11.1"},
		{">=20", "v23.3.0"},
		{">= 20 <22", "v21.7.3"},
		{">20", "v23.3.0"},
		{"<20", "v18.19.1"},
		{"<=20", "v20.11.1"},
		{"^18.17", "v18.19.1"},
		{"^18.20", ""},
		{"~18.16", "v18.16.0"},
		{"~18", "v18.19.1"},
		{"16 || 18", "v18.19.1"},
		{"18 - 20", "v20.11.1"},
		{"*", "v23.3.0"},
		{"latest", "v23.3.0"},
		{"node", "v23.3.0"},
		{"lts/*", "v22.11.0"},
		{"lts", "v22.11.0"},
		{"lts/iron", "v20.11.1"},
		{"LTS/Hydrogen", "v18.19.1"},
		{"lts/gallium", "v16.20.2"},
		{"lts/argon", ""},
		{"lts/unknown", ""},
		{"25", ""},
		{"system", ""},
		{"not a version", ""},
	}
	for _, tt := range tests {
		got, ok := Resolve(tt.want, installed)
		if tt.resolved == "" {
			if ok {
				t.Errorf("Resolve(%q) = %s, want no match", tt.want, got)
			}
			continue
		}
		if !ok || got.String() != tt.resolved {
			t.Errorf("Resolve(%q) = %s, %v; want %s", tt.want, got, ok, tt.resolved)
		}
	}
}

func TestResolve_DefaultPrefersNewestLTS(t *testing.T) {
	got, ok := Resolve("", installedVersions(t, "v20.11.1", "v22.11.0", "v23.3.0"))
	if !ok || got.String() != "v22.11.0" {
		t.Fatalf("default = %s, %v; want v22.11.0", got, ok)
	}
}

func TestResolve_DefaultFallsBackToNewest(t *testing.T) {
	got, ok := Resolve("  ", installedVersions(t, "v21.7.3", "v23.3.0"))
	if !ok || got.String() != "v23.3.0" {
		t.Fatalf("default = %s, %v; want v23.3.0", got, ok)
	}
}

func TestResolve_NothingInstalled(t *testing.T) {
	if v, ok := Resolve("", nil); ok {
		t.Fatalf("default with nothing installed = %s, want no match", v)
	}
	if v, ok := Resolve("20", nil); ok {
		t.Fatalf("20 with nothing installed = %s, want no match", v)
	}
}

func TestValid(t *testing.T) {
	for _, s := range []string{"20", "v20.11.1", "lts/*", "lts/iron", ">=20", "^18.17", "18 || 20", "latest"} {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "system", "lts/nope", "20.1.2.3", "abc", ">=abc"} {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
