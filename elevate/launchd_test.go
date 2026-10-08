package elevate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeLaunchctl struct {
	calls []string

	loaded          bool
	printsAfterBoot int
	bootstrapFails  int
	kickstartOK     bool
	kickstartCalls  int
	bootstrapCalls  int
	bootoutCalls    int
}

func (f *fakeLaunchctl) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "enable":
		return nil, nil
	case "print":
		if f.loaded {
			if f.printsAfterBoot > 0 {
				f.printsAfterBoot--
				if f.printsAfterBoot == 0 {
					f.loaded = false
				}
			}
			return []byte("loaded"), nil
		}
		return []byte("Could not find service"), errors.New("exit status 1")
	case "bootout":
		f.bootoutCalls++
		return nil, nil
	case "bootstrap":
		f.bootstrapCalls++
		if f.loaded {
			return []byte("Bootstrap failed: 5: Input/output error\nTry re-running the command as root for richer errors.\n"), errors.New("exit status 5")
		}
		if f.bootstrapFails > 0 {
			f.bootstrapFails--
			return []byte("Bootstrap failed: 5: Input/output error\n"), errors.New("exit status 5")
		}
		f.loaded = true
		return nil, nil
	case "kickstart":
		f.kickstartCalls++
		if f.kickstartOK && f.loaded {
			return nil, nil
		}
		return []byte("Operation already in progress"), errors.New("exit status 37")
	default:
		return nil, errors.New("unknown launchctl op " + args[0])
	}
}

func testLoadCfg(f *fakeLaunchctl) launchdLoadConfig {
	return launchdLoadConfig{
		run:            f.run,
		sleep:          func(time.Duration) {},
		unloadPolls:    8,
		bootstrapTries: 6,
	}
}

func TestLoadLaunchdJob_BootstrapsWhenNotLoaded(t *testing.T) {
	f := &fakeLaunchctl{}
	if err := loadLaunchdJob("gui/501", LaunchAgentLabel, "/tmp/ai.devctl.plist", testLoadCfg(f)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.bootoutCalls != 0 {
		t.Fatalf("bootout calls = %d, want 0 when job is not loaded", f.bootoutCalls)
	}
	if f.bootstrapCalls != 1 {
		t.Fatalf("bootstrap calls = %d, want 1", f.bootstrapCalls)
	}
	if !f.loaded {
		t.Fatal("job should be loaded after bootstrap")
	}
}

func TestLoadLaunchdJob_WaitsForUnloadBeforeBootstrap(t *testing.T) {
	f := &fakeLaunchctl{loaded: true, printsAfterBoot: 3}
	if err := loadLaunchdJob("gui/501", LaunchAgentLabel, "/tmp/ai.devctl.plist", testLoadCfg(f)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.bootoutCalls != 1 {
		t.Fatalf("bootout calls = %d, want 1", f.bootoutCalls)
	}
	if f.bootstrapCalls != 1 {
		t.Fatalf("bootstrap calls = %d, want 1 after drain", f.bootstrapCalls)
	}
	var sawBootout, sawBootstrap bool
	for _, c := range f.calls {
		if strings.HasPrefix(c, "bootout ") {
			sawBootout = true
			if sawBootstrap {
				t.Fatal("bootstrap ran before bootout")
			}
		}
		if strings.HasPrefix(c, "bootstrap ") {
			if !sawBootout {
				t.Fatal("bootstrap ran without bootout of a loaded job")
			}
			sawBootstrap = true
		}
	}
	if !sawBootstrap {
		t.Fatal("missing bootstrap")
	}
}

func TestLoadLaunchdJob_RetriesBootstrapAfterEIO(t *testing.T) {
	f := &fakeLaunchctl{bootstrapFails: 2}
	if err := loadLaunchdJob("gui/501", LaunchAgentLabel, "/tmp/ai.devctl.plist", testLoadCfg(f)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.bootstrapCalls != 3 {
		t.Fatalf("bootstrap calls = %d, want 3 (2 EIO then success)", f.bootstrapCalls)
	}
}

func TestLoadLaunchdJob_KickstartWhenAlreadyLoaded(t *testing.T) {
	f := &fakeLaunchctl{bootstrapFails: 8, loaded: false, kickstartOK: true}
	// First print: not loaded. Bootstrap fails; a later print sees it loaded
	// (login session autoload) and kickstart succeeds.
	origRun := f.run
	prints := 0
	cfg := testLoadCfg(f)
	cfg.run = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "print" {
			prints++
			if prints > 1 {
				f.loaded = true
			}
		}
		return origRun(args...)
	}
	if err := loadLaunchdJob("gui/501", LaunchAgentLabel, "/tmp/ai.devctl.plist", cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.kickstartCalls < 1 {
		t.Fatal("expected kickstart after autoload")
	}
}

func TestLoadLaunchdJob_RejectsEmptyArgs(t *testing.T) {
	cfg := testLoadCfg(&fakeLaunchctl{})
	if err := loadLaunchdJob("", LaunchAgentLabel, "/tmp/x.plist", cfg); err == nil {
		t.Fatal("expected error for empty domain")
	}
	if err := loadLaunchdJob("gui/501", "", "/tmp/x.plist", cfg); err == nil {
		t.Fatal("expected error for empty label")
	}
	if err := loadLaunchdJob("gui/501", LaunchAgentLabel, "", cfg); err == nil {
		t.Fatal("expected error for empty plist")
	}
}

func TestLoadLaunchdJob_ReturnsBootstrapErrorAfterRetries(t *testing.T) {
	f := &fakeLaunchctl{bootstrapFails: 100}
	err := loadLaunchdJob("gui/501", LaunchAgentLabel, "/tmp/ai.devctl.plist", testLoadCfg(f))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "launchctl bootstrap") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "Input/output error") {
		t.Fatalf("error should include launchctl output, got %v", err)
	}
}
