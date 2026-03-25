// Package runuser provides a helper for running shell commands as a non-root OS user.
package runuser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RunAsUserW runs a shell command as the given OS user via `sudo -u <username>`,
// streaming combined stdout+stderr to w and also returning the full output string.
// home is set as HOME in the subprocess environment so that tools like composer
// and npm resolve ~ correctly.
// dir, if non-empty, is used as the working directory (via `cd` in the shell command).
func RunAsUserW(ctx context.Context, w io.Writer, username, home, dir, command string) (string, error) {
	var shellCmd string
	if dir != "" {
		shellCmd = fmt.Sprintf("cd '%s' && %s", dir, command)
	} else {
		shellCmd = command
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
