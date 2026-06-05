package php

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var installedPatchVersionRe = regexp.MustCompile(`PHP\s+(\d+\.\d+\.\d+)`)

func InstalledPatchVersion(ver, serverRoot string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, filepath.Join(PHPDir(ver, serverRoot), "php"), "-v")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("php %s -v: %w", ver, err)
	}
	patch, err := ParsePatchVersion(out.String())
	if err != nil {
		return "", fmt.Errorf("php %s -v: %w", ver, err)
	}
	return patch, nil
}

func ParsePatchVersion(output string) (string, error) {
	s := bufio.NewScanner(strings.NewReader(output))
	if !s.Scan() {
		return "", fmt.Errorf("empty version output")
	}
	line := s.Text()
	match := installedPatchVersionRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return "", fmt.Errorf("could not parse patch version from %q", line)
	}
	return match[1], nil
}
