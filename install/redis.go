package install

import (
	"context"
	"fmt"
	"io"
)

// RedisInstaller installs Redis from the official packages.redis.io repository.
// Ref: https://redis.io/docs/latest/operate/oss_and_stack/install/archive/install-redis/install-redis-on-linux/
type RedisInstaller struct{}

func (r *RedisInstaller) ServiceID() string { return "redis" }

func (r *RedisInstaller) IsInstalled() bool { return fileExists("/usr/bin/redis-server") }

func (r *RedisInstaller) Install(ctx context.Context) error {
	return r.InstallW(ctx, io.Discard)
}

func (r *RedisInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if r.IsInstalled() {
		return nil
	}

	if err := aptInstallW(ctx, w, "lsb-release", "curl", "gpg"); err != nil {
		return err
	}

	const keyPath = "/usr/share/keyrings/redis-archive-keyring.gpg"
	if err := curlPipeW(ctx, w,
		"https://packages.redis.io/gpg",
		"gpg", "--dearmor", "-o", keyPath,
	); err != nil {
		return err
	}
	if err := chmodFile(keyPath, 0644); err != nil {
		return err
	}

	// Build the APT source line using the current distro codename.
	codename, err := lsbReleaseName(ctx)
	if err != nil {
		return err
	}
	source := fmt.Sprintf(
		"deb [signed-by=%s] https://packages.redis.io/deb %s main\n",
		keyPath, codename,
	)
	const listPath = "/etc/apt/sources.list.d/redis.list"
	if err := writeFile(listPath, source, 0644); err != nil {
		return err
	}

	if err := aptUpdateW(ctx, w); err != nil {
		return err
	}
	if err := aptInstallW(ctx, w, "redis"); err != nil {
		return err
	}

	return enableAndStartW(ctx, w, "redis-server")
}

func (r *RedisInstaller) Purge(ctx context.Context) error {
	return r.PurgeW(ctx, io.Discard)
}

func (r *RedisInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	stopAndDisableW(ctx, w, "redis-server")
	if err := aptPurgeW(ctx, w, "redis", "redis-server", "redis-tools"); err != nil {
		return err
	}
	removeFiles(
		"/usr/share/keyrings/redis-archive-keyring.gpg",
		"/etc/apt/sources.list.d/redis.list",
	)
	return nil
}
