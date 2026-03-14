package install

import (
	"context"
	"io"
)

// DNSInstaller is a no-op installer for the built-in DNS server service.
// The DNS server is an embedded goroutine — there is nothing to install or
// purge. IsInstalled() always returns true so the auto-start loop starts it.
type DNSInstaller struct{}

func (d *DNSInstaller) ServiceID() string { return "dns" }
func (d *DNSInstaller) IsInstalled() bool { return true }

func (d *DNSInstaller) Install(_ context.Context) error { return nil }
func (d *DNSInstaller) Purge(_ context.Context) error   { return nil }

func (d *DNSInstaller) InstallW(_ context.Context, w io.Writer) error {
	_, _ = w.Write([]byte("DNS server is built-in — nothing to install.\n"))
	return nil
}

func (d *DNSInstaller) PurgeW(_ context.Context, w io.Writer) error {
	_, _ = w.Write([]byte("DNS server is built-in — nothing to purge.\n"))
	return nil
}
