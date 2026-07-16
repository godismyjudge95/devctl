package install

import "testing"

func TestMergeSharedPreloadLibraries_AppendWhenMissing(t *testing.T) {
	conf := "listen_addresses = '127.0.0.1'\nport = 5432\n"
	got, changed := mergeSharedPreloadLibraries(conf, "timescaledb")
	if !changed {
		t.Fatal("expected conf to change")
	}
	if !containsLine(got, "shared_preload_libraries = 'timescaledb'") {
		t.Fatalf("expected timescaledb preload, got:\n%s", got)
	}
}

func TestMergeSharedPreloadLibraries_Idempotent(t *testing.T) {
	conf := "shared_preload_libraries = 'timescaledb'\n"
	got, changed := mergeSharedPreloadLibraries(conf, "timescaledb")
	if changed {
		t.Fatalf("expected no change, got:\n%s", got)
	}
}

func TestMergeSharedPreloadLibraries_MergesExisting(t *testing.T) {
	conf := "shared_preload_libraries = 'pg_stat_statements'\n"
	got, changed := mergeSharedPreloadLibraries(conf, "timescaledb")
	if !changed {
		t.Fatal("expected conf to change")
	}
	if !containsLine(got, "shared_preload_libraries = 'pg_stat_statements,timescaledb'") {
		t.Fatalf("expected merged list, got:\n%s", got)
	}
}

func TestMergeSharedPreloadLibraries_IgnoresCommented(t *testing.T) {
	conf := "#shared_preload_libraries = 'something'\nlisten_addresses = '*'\n"
	got, changed := mergeSharedPreloadLibraries(conf, "timescaledb")
	if !changed {
		t.Fatal("expected conf to change")
	}
	// Should append active setting, not uncomment the old one.
	if !containsLine(got, "shared_preload_libraries = 'timescaledb'") {
		t.Fatalf("expected active timescaledb line, got:\n%s", got)
	}
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start <= len(s) {
		out = append(out, s[start:])
	}
	return out
}
