package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatal("need HOME")
	}
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"~", home},
		{"$HOME", home},
		{"~/Code/sites", filepath.Join(home, "Code/sites")},
		{"$HOME/Code/sites/", filepath.Join(home, "Code/sites")},
		{"/abs/path", "/abs/path"},
	}
	for _, c := range cases {
		got := ExpandHome(c.in)
		if got != c.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultSitesDirFor(t *testing.T) {
	linux := DefaultSitesDirFor("linux", "/home/alice")
	if linux != "/home/alice/ddev/sites" {
		t.Errorf("linux = %q", linux)
	}
	darwin := DefaultSitesDirFor("darwin", "/Users/alice")
	if darwin != "/Users/alice/Code/sites" {
		t.Errorf("darwin = %q", darwin)
	}
}
