// Package runuser provides a helper for running shell commands as a non-root OS user.
package runuser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// RunAsUserW runs a shell command as the given OS user via `sudo -u <username>`,
// streaming combined stdout+stderr to w and also returning the full output string.
// home is set as HOME in the subprocess environment so that tools like composer
// and npm resolve ~ correctly. The Composer global bin directory
// ({home}/.config/composer/vendor/bin) is prepended to PATH inside the shell
// command itself (not via the environment), so it survives sudo's secure_path /
// env_reset stripping. Globally installed tools such as `laravel` and `statamic`
// are therefore accessible when devctl runs commands as the site user.
// dir, if non-empty, is used as the working directory (via `cd` in the shell command).
func RunAsUserW(ctx context.Context, w io.Writer, username, home, dir, command string) (string, error) {
	composerBin := filepath.Join(home, ".config", "composer", "vendor", "bin")

	// Build the shell command.  We prepend the PATH assignment directly into
	// the sh -c script so that it takes effect even when sudo resets PATH via
	// secure_path / env_reset.
	var shellCmd string
	pathPrefix := fmt.Sprintf("PATH='%s':\"$PATH\"", composerBin)
	if dir != "" {
		shellCmd = fmt.Sprintf("%s && cd '%s' && %s", pathPrefix, dir, command)
	} else {
		shellCmd = fmt.Sprintf("%s && %s", pathPrefix, command)
	}
	cmd := exec.CommandContext(ctx, "sudo", "-u", username, "--", "sh", "-c", shellCmd)
	// Provide a minimal but correct environment: HOME must point to the
	// user's home so that tools like composer and npm resolve ~ correctly.
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USER="+username,
	)
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	err := cmd.Run()
	return buf.String(), err
}
