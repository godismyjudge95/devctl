package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Spec is the Node version a project asks for.
type Spec struct {
	// Version is the value as written ("20", "lts/iron", ">=20"). Empty when
	// no file names a version.
	Version string `json:"version"`
	// File is the absolute path of the file Version came from.
	File string `json:"file"`
}

// versionFiles are read in this order in each directory, the same order fnm
// and nvm use.
var versionFiles = []struct {
	name string
	read func([]byte) string
}{
	{".nvmrc", firstLine},
	{".node-version", firstLine},
	{"package.json", enginesNode},
}

// Find returns the first version named in dir or, failing that, in its
// nearest ancestor that names one.
func Find(dir string) Spec {
	d := filepath.Clean(dir)
	for {
		for _, f := range versionFiles {
			path := filepath.Join(d, f.name)
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if v := f.read(b); v != "" {
				return Spec{Version: v, File: path}
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return Spec{}
		}
		d = parent
	}
}

// ErrInvalidVersion is returned for a version devctl cannot resolve.
var ErrInvalidVersion = errors.New("invalid Node version")

// WriteNvmrc pins dir to version by writing dir/.nvmrc.
func WriteNvmrc(dir, version string) error {
	version = strings.TrimSpace(version)
	if strings.ContainsAny(version, "\r\n") || !Valid(version) {
		return fmt.Errorf("%w %q", ErrInvalidVersion, version)
	}
	return os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte(version+"\n"), 0644)
}

func firstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func enginesNode(b []byte) string {
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return ""
	}
	return strings.TrimSpace(pkg.Engines.Node)
}
