package php

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrXdebugUnavailable means the installed PHP build has no xdebug.so.
var ErrXdebugUnavailable = errors.New("Xdebug is not available in this PHP build")

// DefaultXdebugMode is the mode used when Xdebug is turned on without one.
const DefaultXdebugMode = "debug"

var xdebugModes = []string{"develop", "coverage", "debug", "gcstats", "profile", "trace"}

// XdebugOffKeys are the directives that load Xdebug. Turning Xdebug off
// removes only these, so other xdebug.* settings apply again next time.
var XdebugOffKeys = []string{"zend_extension", "xdebug.mode"}

// XdebugStatus is the Xdebug state of one PHP version.
type XdebugStatus struct {
	Available bool   `json:"available"`
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
}

// XdebugPath is where a PHP build's shared Xdebug extension lives. PHP 7.x
// (static musl, no dlopen) and releases before Xdebug do not ship one.
func XdebugPath(ver, serverRoot string) string {
	return filepath.Join(PHPDir(ver, serverRoot), "xdebug.so")
}

// ValidateXdebugMode accepts a comma-separated list of Xdebug modes.
func ValidateXdebugMode(mode string) error {
	for m := range strings.SplitSeq(mode, ",") {
		if !slices.Contains(xdebugModes, m) {
			return fmt.Errorf("invalid Xdebug mode %q: use one or more of %s", m, strings.Join(xdebugModes, ", "))
		}
	}
	return nil
}

// XdebugStatusFor reads the Xdebug state of ver from its effective
// directives (see EffectiveDirectives).
func XdebugStatusFor(ver, serverRoot string, effective map[string]string) XdebugStatus {
	path := XdebugPath(ver, serverRoot)
	_, err := os.Stat(path)
	st := XdebugStatus{Available: err == nil}
	if effective["zend_extension"] == path {
		st.Enabled = true
		st.Mode = effective["xdebug.mode"]
	}
	return st
}

// XdebugOnDirectives returns the version directives that load Xdebug in
// mode. Step debugging starts only on a trigger (the browser extension's
// XDEBUG_SESSION cookie) unless the user already chose otherwise.
func XdebugOnDirectives(ver, serverRoot, mode string, effective map[string]string) map[string]string {
	kv := map[string]string{
		"zend_extension": XdebugPath(ver, serverRoot),
		"xdebug.mode":    mode,
	}
	if _, set := effective["xdebug.start_with_request"]; !set {
		kv["xdebug.start_with_request"] = "trigger"
	}
	return kv
}
