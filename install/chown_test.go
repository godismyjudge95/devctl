package install

import (
	"os/user"
	"strings"
	"testing"
)

func TestChownRecursiveCmdUsesPrimaryGroup(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	got := chownRecursiveCmd(u.Username, "/tmp/devctl-chown")
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "chown -R " + u.Username + ":" + g.Name + " "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("got %q, want prefix %q", got, wantPrefix)
	}
}
