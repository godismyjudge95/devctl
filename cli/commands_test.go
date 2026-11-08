package cli

import (
	"testing"
)

// cmdCheck is a concise helper that asserts a command is registered,
// has a non-empty Description and a non-nil Handler, and optionally
// that it declares at least one Arg or Flag.
type cmdCheck struct {
	name     string
	wantArgs bool   // true → len(Args) > 0
	wantFlag string // non-empty → at least one FlagDef with this Name
}

func assertCmd(t *testing.T, tc cmdCheck) {
	t.Helper()
	cmd := Find(tc.name)
	if cmd == nil {
		t.Fatalf("%s: command is not registered", tc.name)
	}
	if cmd.Name != tc.name {
		t.Errorf("%s: Name = %q", tc.name, cmd.Name)
	}
	if cmd.Description == "" {
		t.Errorf("%s: Description is empty", tc.name)
	}
	if cmd.Handler == nil {
		t.Errorf("%s: Handler is nil", tc.name)
	}
	if tc.wantArgs && len(cmd.Args) == 0 {
		t.Errorf("%s: expected at least one ArgDef, got none", tc.name)
	}
	if tc.wantFlag != "" {
		found := false
		for _, f := range cmd.Flags {
			if f.Name == tc.wantFlag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: expected flag %q, not found in Flags", tc.name, tc.wantFlag)
		}
	}
}

// ---------------------------------------------------------------------------
// dns
// ---------------------------------------------------------------------------

func TestDNSStatus_IsRegistered(t *testing.T)   { assertCmd(t, cmdCheck{name: "dns:status"}) }
func TestDNSSetup_IsRegistered(t *testing.T)    { assertCmd(t, cmdCheck{name: "dns:setup"}) }
func TestDNSTeardown_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "dns:teardown"}) }

// ---------------------------------------------------------------------------
// dumps
// ---------------------------------------------------------------------------

func TestHelpersList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "helpers:list"}) }
func TestHelpersAvailable_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "helpers:available"})
}
func TestHelpersInstall_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "helpers:install", wantArgs: true})
}
func TestHelpersUpdate_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "helpers:update", wantArgs: true})
}
func TestHelpersUninstall_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "helpers:uninstall", wantArgs: true})
}

func TestNodeList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "node:list"}) }
func TestNodeInstall_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "node:install", wantArgs: true})
}
func TestNodeUninstall_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "node:uninstall", wantArgs: true})
}

func TestDumpsList_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "dumps:list", wantFlag: "domain"})
}
func TestDumpsClear_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "dumps:clear"}) }

// ---------------------------------------------------------------------------
// logs
// ---------------------------------------------------------------------------

func TestLogsList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "logs:list"}) }
func TestLogsTail_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "logs:tail", wantArgs: true, wantFlag: "bytes"})
}
func TestLogsTail_HasFollowFlag(t *testing.T) {
	assertCmd(t, cmdCheck{name: "logs:tail", wantFlag: "follow"})
}
func TestLogsClear_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "logs:clear", wantArgs: true})
}

// ---------------------------------------------------------------------------
// mail
// ---------------------------------------------------------------------------

func TestMailList_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:list", wantFlag: "limit"})
}
func TestMailGet_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:get", wantArgs: true})
}
func TestMailDelete_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:delete", wantArgs: true})
}
func TestMailClear_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "mail:clear"}) }
func TestMailList_HasFilterFlags(t *testing.T) {
	for _, f := range []string{"to", "from", "subject", "since"} {
		assertCmd(t, cmdCheck{name: "mail:list", wantFlag: f})
	}
}
func TestMailGet_HasFormatFlag(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:get", wantArgs: true, wantFlag: "format"})
}
func TestMailLatest_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:latest", wantFlag: "to"})
}
func TestMailWait_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:wait", wantFlag: "timeout"})
}
func TestMailLinks_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:links", wantArgs: true})
}
func TestMailAttachments_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:attachments", wantArgs: true})
}
func TestMailAttachment_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "mail:attachment", wantArgs: true, wantFlag: "out"})
}

// ---------------------------------------------------------------------------
// php
// ---------------------------------------------------------------------------

func TestPHPVersions_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "php:versions"}) }
func TestPHPSettings_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "php:settings"}) }
func TestPHPSet_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "php:set", wantArgs: true, wantFlag: "version"})
}
func TestPHPIniList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "php:ini:list"}) }
func TestPHPIniSet_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "php:ini:set", wantArgs: true})
}
func TestPHPIniUnset_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "php:ini:unset", wantArgs: true})
}

// ---------------------------------------------------------------------------
// settings
// ---------------------------------------------------------------------------

func TestSettingsGet_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "settings:get"}) }
func TestSettingsSet_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "settings:set", wantArgs: true})
}

// ---------------------------------------------------------------------------
// sites
// ---------------------------------------------------------------------------

func TestSitesList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "sites:list"}) }
func TestSitesGet_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:get", wantArgs: true})
}
func TestSitesPHP_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:php", wantArgs: true})
}
func TestSitesSPX_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:spx", wantArgs: true})
}
func TestSitesWorktrees_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:worktrees"})
}
func TestSitesWorktreeAdd_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:worktree:add", wantArgs: true, wantFlag: "create"})
}
func TestSitesWorktreeRm_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:worktree:rm", wantArgs: true})
}
func TestSitesWorktreeConfig_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:worktree:config", wantArgs: true, wantFlag: "copy"})
}
func TestSitesBranches_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:branches", wantArgs: true})
}
func TestSitesConfigInit_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "sites:config:init", wantArgs: true, wantFlag: "force"})
}

// ---------------------------------------------------------------------------
// services (already had tests — include here for completeness)
// ---------------------------------------------------------------------------

func TestServicesList_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "services:list"}) }
func TestServicesStart_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "services:start", wantArgs: true})
}
func TestServicesStop_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "services:stop", wantArgs: true})
}
func TestServicesRestart_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "services:restart", wantArgs: true})
}
func TestServicesCredentials_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "services:credentials", wantArgs: true})
}
func TestServicesUpdate_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "services:update", wantArgs: true})
}

// ---------------------------------------------------------------------------
// spx
// ---------------------------------------------------------------------------

func TestSPXProfiles_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "spx:profiles", wantFlag: "domain"})
}
func TestSPXProfile_IsRegistered(t *testing.T) {
	assertCmd(t, cmdCheck{name: "spx:profile", wantArgs: true})
}

// ---------------------------------------------------------------------------
// tls
// ---------------------------------------------------------------------------

func TestTLSTrust_IsRegistered(t *testing.T) { assertCmd(t, cmdCheck{name: "tls:trust"}) }
