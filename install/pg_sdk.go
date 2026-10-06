package install

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func darwinSDKRoot() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("xcrun", "--show-sdk-path").Output()
	if err == nil {
		if p := strings.TrimSpace(string(out)); p != "" && fileExists(p) {
			return p
		}
	}
	fallback := "/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk"
	if fileExists(fallback) {
		return fallback
	}
	return ""
}

// darwinPGCompileVars overrides the stale -isysroot from EDB pg_config
// (MacOSX14.sdk) with the Command Line Tools SDK that is actually installed.
func darwinPGCompileVars() string {
	sdk := darwinSDKRoot()
	if sdk == "" {
		return ""
	}
	flag := "-isysroot " + sdk
	return fmt.Sprintf(
		"SDKROOT=%s CMAKE_OSX_SYSROOT=%s CPPFLAGS=%s CFLAGS=%s LDFLAGS=%s PG_CPPFLAGS=%s PG_CFLAGS=%s PG_LDFLAGS=%s",
		shellQuote(sdk), shellQuote(sdk),
		shellQuote(flag), shellQuote(flag), shellQuote(flag),
		shellQuote(flag), shellQuote(flag), shellQuote(flag),
	)
}

func darwinPGXSMakeArgs() string {
	sdk := darwinSDKRoot()
	if sdk == "" {
		return ""
	}
	// EDB Makefile.global hardcodes PG_SYSROOT=MacOSX14.sdk. Command-line
	// PG_SYSROOT replaces it. Do not pass PG_CPPFLAGS — extensions such as
	// pg_clickhouse set their own include paths in that variable.
	return "PG_SYSROOT=" + shellQuote(sdk)
}

func darwinCMakeSysrootArgs() string {
	sdk := darwinSDKRoot()
	if sdk == "" {
		return ""
	}
	flag := "-isysroot " + sdk
	return fmt.Sprintf(
		" -DCMAKE_OSX_SYSROOT=%s -DCMAKE_SHARED_LINKER_FLAGS=%s -DCMAKE_MODULE_LINKER_FLAGS=%s -DCMAKE_EXE_LINKER_FLAGS=%s",
		shellQuote(sdk), shellQuote(flag), shellQuote(flag), shellQuote(flag),
	)
}

// writeDarwinPgConfigShim wraps pg_config so -isysroot points at the live SDK.
// EDB ships MacOSX14.sdk in --cppflags/--ldflags; that path is gone on newer CLT.
func writeDarwinPgConfigShim(dir, realPgConfig string) (string, error) {
	if runtime.GOOS != "darwin" {
		return realPgConfig, nil
	}
	sdk := darwinSDKRoot()
	if sdk == "" {
		return realPgConfig, nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	shim := filepath.Join(dir, "pg_config")
	script := fmt.Sprintf("#!/bin/sh\n%s \"$@\" | /usr/bin/sed 's|-isysroot [^ ]*|-isysroot %s|g'\n",
		shellQuote(realPgConfig), sdk)
	if err := os.WriteFile(shim, []byte(script), 0755); err != nil {
		return "", err
	}
	return shim, nil
}

var staleMacSDK = regexp.MustCompile(`/Library/Developer/CommandLineTools/SDKs/MacOSX[0-9][^/\s"]*\.sdk`)

func rewriteStaleDarwinSysroot(root string) error {
	sdk := darwinSDKRoot()
	if sdk == "" {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch info.Name() {
		case "CMakeCache.txt", "flags.make", "link.txt", "build.make", "depend.make":
		default:
			if !strings.HasSuffix(info.Name(), ".ninja") {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		next := staleMacSDK.ReplaceAll(data, []byte(sdk))
		if bytes.Equal(data, next) {
			return nil
		}
		return os.WriteFile(path, next, info.Mode())
	})
}
