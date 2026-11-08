package php

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

// GlobalScope is the directive scope that applies to every installed version.
const GlobalScope = "all"

// Directive is one php.ini key that devctl writes for a scope: GlobalScope or
// a PHP minor version such as "8.4".
type Directive struct {
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

const (
	managedBlockBegin = "; >>> devctl managed directives (devctl php:ini:set / php:ini:unset). Edits here are overwritten. >>>"
	managedBlockEnd   = "; <<< devctl managed directives <<<"
)

var iniMu sync.Mutex

// LockIni takes the lock every FPM php.ini writer shares and returns its
// unlock. Callers of WriteManagedBlock and raw php.ini writes hold it.
func LockIni() (unlock func()) {
	iniMu.Lock()
	return iniMu.Unlock
}

var directiveKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*$`)

// ValidateScope accepts GlobalScope or a PHP minor version ("8.4").
func ValidateScope(scope string) error {
	if scope == GlobalScope || versionRe.MatchString(scope) {
		return nil
	}
	return fmt.Errorf("invalid scope %q: use %q or a PHP version such as 8.4", scope, GlobalScope)
}

// ValidateDirective rejects keys that are not php.ini names and values that
// would break out of their line in the managed block.
func ValidateDirective(key, value string) error {
	if !directiveKeyRe.MatchString(key) {
		return fmt.Errorf("invalid php.ini key %q", key)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("value for %s must be a single line", key)
	}
	return nil
}

// EffectiveDirectives returns the directives that apply to scope. For a
// version, its own directives win over GlobalScope ones with the same key.
// For GlobalScope, only the global directives are returned.
func EffectiveDirectives(ds []Directive, scope string) map[string]string {
	out := map[string]string{}
	for _, d := range ds {
		if d.Scope == GlobalScope {
			out[d.Key] = d.Value
		}
	}
	if scope == GlobalScope {
		return out
	}
	for _, d := range ds {
		if d.Scope == scope {
			out[d.Key] = d.Value
		}
	}
	return out
}

// WriteManagedBlock rewrites the devctl block in the php.ini at path so it
// holds exactly directives, sorted by key. The block always goes at the end of
// the file because php.ini is last-assignment-wins. Lines outside the block are
// kept. An empty map removes the block. Callers hold LockIni.
func WriteManagedBlock(path string, directives map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	rest, hadBlock := stripManagedBlock(string(data))
	if !hadBlock && len(directives) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(strings.TrimRight(rest, "\n"))
	sb.WriteString("\n")
	if len(directives) > 0 {
		keys := make([]string, 0, len(directives))
		for k := range directives {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString("\n" + managedBlockBegin + "\n")
		// A [PATH=] or [HOST=] section above would otherwise scope these lines.
		sb.WriteString("[PHP]\n")
		for _, k := range keys {
			sb.WriteString(k + " = " + directives[k] + "\n")
		}
		sb.WriteString(managedBlockEnd + "\n")
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// stripManagedBlock removes the devctl block.
func stripManagedBlock(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	begin, end := managedBlockRange(lines)
	if begin < 0 {
		return content, false
	}
	kept := append(append([]string{}, lines[:begin]...), lines[end+1:]...)
	return strings.Join(kept, "\n"), true
}

// managedBlockRange returns the line indexes of the devctl block's markers,
// or -1, -1 when there is no block. A block whose end marker was deleted runs
// to the last line.
func managedBlockRange(lines []string) (begin, end int) {
	begin = slices.IndexFunc(lines, func(l string) bool { return strings.TrimSpace(l) == managedBlockBegin })
	if begin < 0 {
		return -1, -1
	}
	end = len(lines) - 1
	for i := begin + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == managedBlockEnd {
			end = i
			break
		}
	}
	return begin, end
}

// appendOutsideBlock adds line after the user's lines but before the devctl
// block, so the block's assignments still win.
func appendOutsideBlock(lines []string, line string) []string {
	at, _ := managedBlockRange(lines)
	if at < 0 {
		at = len(lines)
		if at > 0 && lines[at-1] == "" {
			at--
		}
	}
	return slices.Insert(lines, at, line)
}
