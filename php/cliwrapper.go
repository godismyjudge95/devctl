package php

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/danielgormly/devctl/paths"
)

// writeCLIWrapper writes a shell wrapper that loads the version php.ini and
// exports the CA bundle so Artisan, Guzzle, and the AWS SDK trust https://*.test.
func writeCLIWrapper(wrapperPath, cliBin, serverRoot string) error {
	if _, err := os.Stat(cliBin); err != nil {
		return err
	}
	iniPath := filepath.Join(filepath.Dir(cliBin), "php.ini")
	bundlePath := paths.CABundlePath(serverRoot)
	script := "#!/bin/sh\n" +
		"export SSL_CERT_FILE=" + strconv.Quote(bundlePath) + "\n" +
		"export CURL_CA_BUNDLE=" + strconv.Quote(bundlePath) + "\n" +
		"export AWS_CA_BUNDLE=" + strconv.Quote(bundlePath) + "\n" +
		"exec " + strconv.Quote(cliBin) + " -c " + strconv.Quote(iniPath) + " \"$@\"\n"
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0755); err != nil {
		return err
	}
	_ = os.Remove(wrapperPath)
	if err := os.WriteFile(wrapperPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write php wrapper: %w", err)
	}
	return nil
}

// EnsureCLIWrappers writes bin/php{ver} and bin/php wrappers for every
// installed version that has a CLI binary.
func EnsureCLIWrappers(serverRoot string) error {
	versions, err := InstalledVersions(serverRoot)
	if err != nil {
		return err
	}
	binDir := paths.BinDir(serverRoot)
	for _, v := range versions {
		cliBin := filepath.Join(PHPDir(v.Version, serverRoot), "php")
		if _, statErr := os.Stat(cliBin); statErr != nil {
			continue
		}
		if err := writeCLIWrapper(filepath.Join(binDir, "php"+v.Version), cliBin, serverRoot); err != nil {
			return err
		}
	}
	return UpdateGlobalSymlink(serverRoot)
}
