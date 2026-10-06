package tools

import (
	"fmt"
	"testing"

	"github.com/danielgormly/devctl/dist"
)

func TestLinuxAmd64ToolURLTokens(t *testing.T) {
	yq, err := dist.Lookup("linux", "amd64", "yq")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("https://github.com/mikefarah/yq/releases/download/v4.53.6/%s", yq.File)
	want := "https://github.com/mikefarah/yq/releases/download/v4.53.6/yq_linux_amd64"
	if got != want {
		t.Errorf("yq = %q, want %q", got, want)
	}

	mago, err := dist.Lookup("linux", "amd64", "mago")
	if err != nil {
		t.Fatal(err)
	}
	got = fmt.Sprintf("https://github.com/carthage-software/mago/releases/download/1.20.1/mago-1.20.1-%s.tar.gz", mago.Token)
	want = "https://github.com/carthage-software/mago/releases/download/1.20.1/mago-1.20.1-x86_64-unknown-linux-gnu.tar.gz"
	if got != want {
		t.Errorf("mago = %q, want %q", got, want)
	}

	fnm, err := dist.Lookup("linux", "amd64", "fnm")
	if err != nil {
		t.Fatal(err)
	}
	got = fmt.Sprintf("https://github.com/Schniz/fnm/releases/download/v1.39.0/%s", fnm.File)
	want = "https://github.com/Schniz/fnm/releases/download/v1.39.0/fnm-linux.zip"
	if got != want {
		t.Errorf("fnm = %q, want %q", got, want)
	}
}

func TestDarwinArm64ToolURLTokens(t *testing.T) {
	yq, err := dist.Lookup("darwin", "arm64", "yq")
	if err != nil {
		t.Fatal(err)
	}
	if yq.File != "yq_darwin_arm64" {
		t.Errorf("yq file = %q", yq.File)
	}
	mago, err := dist.Lookup("darwin", "arm64", "mago")
	if err != nil {
		t.Fatal(err)
	}
	if mago.Token != "aarch64-apple-darwin" {
		t.Errorf("mago token = %q", mago.Token)
	}
	fnm, err := dist.Lookup("darwin", "arm64", "fnm")
	if err != nil {
		t.Fatal(err)
	}
	if fnm.File != "fnm-macos.zip" {
		t.Errorf("fnm file = %q", fnm.File)
	}
}
