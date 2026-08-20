package versioncache

import "testing"

func TestAvailable_TrueWhenVersionsDiffer(t *testing.T) {
	if !Available("1.0.0", "1.1.0") {
		t.Fatal("expected update available")
	}
}

func TestAvailable_FalseWhenEqual(t *testing.T) {
	if Available("1.0.0", "1.0.0") {
		t.Fatal("expected no update")
	}
}

func TestAvailable_StripsVPrefix(t *testing.T) {
	if Available("2.10.0", "v2.10.0") {
		t.Fatal("v prefix should not make versions differ")
	}
	if !Available("2.10.0", "v2.11.0") {
		t.Fatal("expected update after stripping v")
	}
}

func TestAvailable_FalseWhenEitherEmpty(t *testing.T) {
	if Available("", "1.0.0") {
		t.Fatal("empty installed is unknown, not an update")
	}
	if Available("1.0.0", "") {
		t.Fatal("empty latest is unknown, not an update")
	}
}

func TestCache_SetGetDelete(t *testing.T) {
	c := New()
	c.Set("mago", "1.2.3")
	if got := c.Get("mago"); got != "1.2.3" {
		t.Fatalf("Get = %q, want 1.2.3", got)
	}
	c.Delete("mago")
	if got := c.Get("mago"); got != "" {
		t.Fatalf("Get after delete = %q, want empty", got)
	}
}

func TestCache_SetIgnoresEmpty(t *testing.T) {
	c := New()
	c.Set("mago", "")
	if got := c.Get("mago"); got != "" {
		t.Fatalf("empty version should not be stored, got %q", got)
	}
}

func TestCache_SnapshotIsCopy(t *testing.T) {
	c := New()
	c.Set("a", "1")
	snap := c.Snapshot()
	snap["a"] = "mutated"
	if got := c.Get("a"); got != "1" {
		t.Fatalf("mutating snapshot leaked into cache: %q", got)
	}
}
