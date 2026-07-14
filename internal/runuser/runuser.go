// Package runuser provides a helper for running shell commands as a non-root OS user.
package runuser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// RunAsUserW runs a shell command as the given OS user.
// When the current process is already that user, the command runs directly
// (no sudo). Otherwise it uses `sudo -u <username>`.
//
// Streaming: combined stdout+stderr go to w; the full output string is returned.
// home is set as HOME so tools like composer resolve ~ correctly. The Composer
// global bin directory ({home}/.config/composer/vendor/bin) is prepended to PATH
// inside the shell command so it survives sudo secure_path / env_reset.
// dir, if non-empty, is used as the working directory (via `cd` in the shell).
func RunAsUserW(ctx context.Context, w io.Writer, username, home, dir, command string) (string, error) {
	composerBin := filepath.Join(home, ".config", "composer", "vendor", "bin")

	var shellCmd string
	pathPrefix := fmt.Sprintf("PATH='%s':\"$PATH\"", composerBin)
	if dir != "" {
		shellCmd = fmt.Sprintf("%s && cd '%s' && %s", pathPrefix, dir, command)
	} else {
		shellCmd = fmt.Sprintf("%s && %s", pathPrefix, command)
	}

	var cmd *exec.Cmd
	if isCurrentUser(username) {
		cmd = exec.CommandContext(ctx, "sh", "-c", shellCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sudo", "-u", username, "--", "sh", "-c", shellCmd)
	}
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

func isCurrentUser(username string) bool {
	if username == "" {
		return true
	}
	cu, err := user.Current()
	if err != nil {
		return false
	}
	return cu.Username == username
}
