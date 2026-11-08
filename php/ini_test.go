package php

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgormly/devctl/paths"
)

func writeTempIni(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "php.ini")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteManagedBlock_AppendsSortedBlockAndKeepsUserLines(t *testing.T) {
	p := writeTempIni(t, "[PHP]\nmemory_limit = 128M\n; user comment\n")
	if err := WriteManagedBlock(p, map[string]string{"memory_limit": "1G", "display_errors": "On"}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	want := "[PHP]\nmemory_limit = 128M\n; user comment\n\n" +
		managedBlockBegin + "\n[PHP]\ndisplay_errors = On\nmemory_limit = 1G\n" + managedBlockEnd + "\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteManagedBlock_IsIdempotent(t *testing.T) {
	p := writeTempIni(t, "a = 1\n")
	d := map[string]string{"b": "2"}
	if err := WriteManagedBlock(p, d); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, p)
	if err := WriteManagedBlock(p, d); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, p); second != first {
		t.Errorf("second write changed the file:\n%s\nvs\n%s", first, second)
	}
}

func TestWriteManagedBlock_MovesBlockBelowLaterUserEdits(t *testing.T) {
	p := writeTempIni(t, "a = 1\n")
	if err := WriteManagedBlock(p, map[string]string{"memory_limit": "1G"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("user_after = yes\n")
	f.Close()

	if err := WriteManagedBlock(p, map[string]string{"memory_limit": "2G"}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	if !strings.Contains(got, "user_after = yes") {
		t.Errorf("user line lost:\n%s", got)
	}
	if strings.Count(got, managedBlockBegin) != 1 {
		t.Errorf("want exactly one managed block:\n%s", got)
	}
	if strings.Index(got, "user_after") > strings.Index(got, "memory_limit = 2G") {
		t.Errorf("managed block must come last so it wins:\n%s", got)
	}
	if strings.Contains(got, "memory_limit = 1G") {
		t.Errorf("old managed value left behind:\n%s", got)
	}
}

func TestWriteManagedBlock_EmptyRemovesBlock(t *testing.T) {
	p := writeTempIni(t, "a = 1\n")
	if err := WriteManagedBlock(p, map[string]string{"b": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(p, nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "a = 1\n" {
		t.Errorf("got %q, want %q", got, "a = 1\n")
	}
}

func TestWriteManagedBlock_EmptyLeavesFileWithoutBlockUntouched(t *testing.T) {
	orig := "a = 1\n\n\n"
	p := writeTempIni(t, orig)
	if err := WriteManagedBlock(p, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != orig {
		t.Errorf("file changed: %q", got)
	}
}

func TestEffectiveDirectives_VersionWinsOverGlobal(t *testing.T) {
	ds := []Directive{
		{Scope: GlobalScope, Key: "memory_limit", Value: "512M"},
		{Scope: GlobalScope, Key: "display_errors", Value: "On"},
		{Scope: "8.4", Key: "memory_limit", Value: "1G"},
		{Scope: "8.3", Key: "opcache.jit", Value: "off"},
	}
	got := EffectiveDirectives(ds, "8.4")
	want := map[string]string{"memory_limit": "1G", "display_errors": "On"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	global := EffectiveDirectives(ds, GlobalScope)
	if global["memory_limit"] != "512M" || len(global) != 2 {
		t.Errorf("global scope got %v", global)
	}
}

func TestValidateScope(t *testing.T) {
	for _, ok := range []string{"all", "8.4", "7.0"} {
		if err := ValidateScope(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "8", "8.4.1", "latest", "../8.4"} {
		if err := ValidateScope(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestValidateDirective(t *testing.T) {
	for _, k := range []string{"memory_limit", "opcache.jit", "xdebug.mode", "date.timezone"} {
		if err := ValidateDirective(k, "x"); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	if err := ValidateDirective("error_reporting", "E_ALL & ~E_DEPRECATED"); err != nil {
		t.Errorf("expression value rejected: %v", err)
	}
	for _, k := range []string{"", "1abc", "a b", "a=b", "[PHP]", "a;b"} {
		if err := ValidateDirective(k, "x"); err == nil {
			t.Errorf("key %q: want error", k)
		}
	}
	if err := ValidateDirective("memory_limit", "1G\nauto_prepend_file=/tmp/x"); err == nil {
		t.Error("multi-line value: want error")
	}
}

func TestBaseSettings_IgnoresManagedBlock(t *testing.T) {
	serverRoot := t.TempDir()
	ini := PHPIniPath("8.4", serverRoot)
	if err := os.MkdirAll(filepath.Dir(ini), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ini, []byte("memory_limit = 128M\nmax_execution_time = 30\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(ini, map[string]string{"memory_limit": "1G"}); err != nil {
		t.Fatal(err)
	}

	base, err := BaseSettings("8.4", serverRoot)
	if err != nil {
		t.Fatal(err)
	}
	if base.MemoryLimit != "128M" {
		t.Errorf("base memory_limit = %q, want 128M", base.MemoryLimit)
	}
	eff, err := GetSettings("8.4", serverRoot)
	if err != nil {
		t.Fatal(err)
	}
	if eff.MemoryLimit != "1G" || eff.MaxExecutionTime != "30" {
		t.Errorf("effective = %+v, want memory_limit 1G and max_execution_time 30", eff)
	}
}

func TestGlobalSettings_ValuesSkipsEmptyAndOverlaySetsKnownKeys(t *testing.T) {
	g := GlobalSettings{MemoryLimit: "1G"}
	v := g.Values()
	if len(v) != 1 || v["memory_limit"] != "1G" {
		t.Errorf("Values() = %v", v)
	}
	g.Overlay(map[string]string{"post_max_size": "64M", "display_errors": "On"})
	if g.PostMaxSize != "64M" || g.MemoryLimit != "1G" {
		t.Errorf("Overlay result %+v", g)
	}
}

// lastAssignment returns the value PHP uses for key: the last uncommented
// assignment wins.
func lastAssignment(content, key string) string {
	val := ""
	for _, l := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && !strings.HasPrefix(k, ";") && strings.TrimSpace(k) == key {
			val = strings.TrimSpace(v)
		}
	}
	return val
}

func managedBlockOf(t *testing.T, content string) string {
	t.Helper()
	i := strings.Index(content, managedBlockBegin)
	if i < 0 {
		t.Fatalf("no managed block in:\n%s", content)
	}
	return content[i:]
}

func TestMigrateCAFile_LeavesManagedBlockAlone(t *testing.T) {
	p := writeTempIni(t, "openssl.cafile = /old.pem\n")
	if err := WriteManagedBlock(p, map[string]string{"openssl.cafile": "/user.pem"}); err != nil {
		t.Fatal(err)
	}
	block := managedBlockOf(t, readFile(t, p))

	if err := migrateCAFile(p, "/bundle.crt"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	if managedBlockOf(t, got) != block {
		t.Errorf("managed block changed:\n%s", got)
	}
	if v := lastAssignment(got, "openssl.cafile"); v != "/user.pem" {
		t.Errorf("effective openssl.cafile = %q, want the managed /user.pem", v)
	}
	if v := lastAssignment(got, "curl.cainfo"); v != "/bundle.crt" {
		t.Errorf("effective curl.cainfo = %q, want /bundle.crt", v)
	}
	if strings.Contains(got, "/old.pem") {
		t.Errorf("line outside the block was not updated:\n%s", got)
	}
}

func TestConfigurePrepend_LeavesManagedBlockAlone(t *testing.T) {
	serverRoot := t.TempDir()
	ini := PHPIniPath("8.4", serverRoot)
	if err := os.MkdirAll(filepath.Dir(ini), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ini, []byte("memory_limit = 128M\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteManagedBlock(ini, map[string]string{"auto_prepend_file": "/user/prepend.php"}); err != nil {
		t.Fatal(err)
	}
	block := managedBlockOf(t, readFile(t, ini))

	if err := ConfigurePrepend(context.Background(), "8.4", serverRoot); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, ini)
	if managedBlockOf(t, got) != block {
		t.Errorf("managed block changed:\n%s", got)
	}
	want := "auto_prepend_file = " + paths.PrependPath(serverRoot)
	if i := strings.Index(got, want); i < 0 || i > strings.Index(got, managedBlockBegin) {
		t.Errorf("want %q above the managed block:\n%s", want, got)
	}
	if v := lastAssignment(got, "auto_prepend_file"); v != "/user/prepend.php" {
		t.Errorf("effective auto_prepend_file = %q, want the managed value", v)
	}
}

func TestMigrateSpxKey_LeavesManagedBlockAlone(t *testing.T) {
	p := writeTempIni(t, "spx.http_key =\n")
	if err := WriteManagedBlock(p, map[string]string{"spx.http_key": ""}); err != nil {
		t.Fatal(err)
	}
	block := managedBlockOf(t, readFile(t, p))

	if err := migrateSpxKey(p); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	if managedBlockOf(t, got) != block {
		t.Errorf("managed block changed:\n%s", got)
	}
	if !strings.HasPrefix(got, "spx.http_key = dev\n") {
		t.Errorf("line outside the block was not migrated:\n%s", got)
	}
}
