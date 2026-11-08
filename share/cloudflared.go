package share

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Quick tunnel hosts are hyphenated words, so the API host
// (api.trycloudflare.com) that shows up in error lines never matches.
var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9]+(?:-[a-z0-9]+)+\.trycloudflare\.com`)

// ParseURL returns the quick tunnel URL in one line of cloudflared output, or "".
func ParseURL(line string) string {
	return quickTunnelURL.FindString(line)
}

// errorMessage returns the message of a cloudflared error line, or "".
// cloudflared logs "<timestamp> ERR <message>" and prints fatal errors bare.
func errorMessage(line string) string {
	if _, msg, ok := strings.Cut(line, " ERR "); ok {
		return strings.TrimSpace(msg)
	}
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "failed") || strings.HasPrefix(lower, "error") {
		return strings.TrimSpace(line)
	}
	return ""
}

// CloudflaredArgs builds a quick tunnel to Caddy's HTTPS listener.
//
// The tunnel sends the site's own Host and SNI, so the existing Caddy vhost
// matches and PHP sees the .test host with HTTPS=on. The per-site Force HTTPS
// redirect matches only plain HTTP, so it never fires on this hop. cloudflared
// moves the public host into X-Forwarded-Host. TLS verification is off because
// the hop is loopback to Caddy's local CA.
func CloudflaredArgs(domain string, httpsPort int) []string {
	return []string{
		"tunnel",
		"--no-autoupdate",
		"--grace-period", "1s",
		"--url", fmt.Sprintf("https://127.0.0.1:%d", httpsPort),
		"--http-host-header", domain,
		"--origin-server-name", domain,
		"--no-tls-verify",
	}
}

// Cloudflared returns a Launcher that runs the cloudflared binary at bin.
func Cloudflared(bin string, httpsPort int) Launcher {
	return func(domain string, out *LineWriter) (Process, error) {
		cmd := exec.Command(bin, CloudflaredArgs(domain, httpsPort)...)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start cloudflared: %w", err)
		}
		return &execProcess{cmd: cmd, done: make(chan struct{})}, nil
	}
}

type execProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *execProcess) Wait() error {
	err := p.cmd.Wait()
	close(p.done)
	return err
}

func (p *execProcess) Stop() {
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}
