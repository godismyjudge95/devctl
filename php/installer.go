package php

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const aptTimeout = 5 * time.Minute

// DefaultExtensions is the list installed with every new PHP version.
var DefaultExtensions = []string{
	"bcmath", "curl", "gd", "imagick", "intl", "mbstring",
	"mysql", "pgsql", "redis", "sqlite3", "xml", "xmlwriter",
	"zip", "opcache", "readline", "soap",
}

// Install runs `apt-get install php{ver}-fpm` plus the given extensions.
// After installation the FPM process will be started by the caller via the
// supervisor; this function no longer calls systemctl.
func Install(ctx context.Context, ver string, extensions []string) error {
	ctx, cancel := context.WithTimeout(ctx, aptTimeout)
	defer cancel()

	pkgs := []string{fmt.Sprintf("php%s-fpm", ver)}
	for _, ext := range extensions {
		pkgs = append(pkgs, fmt.Sprintf("php%s-%s", ver, ext))
	}

	args := append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, pkgs...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apt-get install: %w\n%s", err, out.String())
	}

	// Configure auto_prepend_file for the dump server.
	if err := ConfigurePrepend(ctx, ver); err != nil {
		// Non-fatal — log but don't fail the install.
		fmt.Printf("php: configure prepend for %s: %v\n", ver, err)
	}

	return nil
}

// Uninstall purges php{ver}-fpm and all php{ver}-* packages.
// The caller is responsible for stopping the supervised process before calling
// this function.
func Uninstall(ctx context.Context, ver string) error {
	ctx, cancel := context.WithTimeout(ctx, aptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		"apt-get", "purge", "-y",
		fmt.Sprintf("php%s-*", ver),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apt-get purge: %w\n%s", err, out.String())
	}
	return nil
}

// InstallExtension installs a single extension for a given PHP version.
func InstallExtension(ctx context.Context, ver, ext string) error {
	ctx, cancel := context.WithTimeout(ctx, aptTimeout)
	defer cancel()

	pkg := fmt.Sprintf("php%s-%s", ver, ext)
	cmd := exec.CommandContext(ctx, "apt-get", "install", "-y", pkg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s: %w\n%s", pkg, err, out.String())
	}
	return nil
}

// RemoveExtension removes a single extension for a given PHP version.
func RemoveExtension(ctx context.Context, ver, ext string) error {
	ctx, cancel := context.WithTimeout(ctx, aptTimeout)
	defer cancel()

	pkg := fmt.Sprintf("php%s-%s", ver, ext)
	cmd := exec.CommandContext(ctx, "apt-get", "purge", "-y", pkg)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("purge %s: %w\n%s", pkg, err, out.String())
	}
	return nil
}
