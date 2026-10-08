package elevate

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
)

type launchdLoadConfig struct {
	run            func(args ...string) ([]byte, error)
	sleep          func(time.Duration)
	unloadPolls    int
	bootstrapTries int
}

func defaultLaunchdLoadConfig() launchdLoadConfig {
	return launchdLoadConfig{
		run: func(args ...string) ([]byte, error) {
			return exec.Command("launchctl", args...).CombinedOutput()
		},
		sleep:          time.Sleep,
		unloadPolls:    100, // 10s at 100ms
		bootstrapTries: 20,  // 5s at 250ms
	}
}

// LoadLaunchdJob unloads a prior job if it is still in the domain, waits until
// launchd has finished tearing it down, then bootstraps plist. Immediate
// bootstrap after bootout fails with EIO (exit 5) while the old job is
// SIGTERMed.
func LoadLaunchdJob(domain, label, plist string) error {
	return loadLaunchdJob(domain, label, plist, defaultLaunchdLoadConfig())
}

func loadLaunchdJob(domain, label, plist string, cfg launchdLoadConfig) error {
	if domain == "" || label == "" || plist == "" {
		return errors.New("launchctl: domain, label, and plist are required")
	}
	if cfg.run == nil {
		return errors.New("launchctl: missing runner")
	}
	if cfg.sleep == nil {
		cfg.sleep = time.Sleep
	}
	if cfg.unloadPolls <= 0 {
		cfg.unloadPolls = 100
	}
	if cfg.bootstrapTries <= 0 {
		cfg.bootstrapTries = 20
	}

	target := domain + "/" + label
	_, _ = cfg.run("enable", target)

	if _, err := cfg.run("print", target); err == nil {
		_, _ = cfg.run("bootout", target)
		if err := waitLaunchdGone(target, cfg); err != nil {
			if _, err2 := cfg.run("kickstart", "-k", target); err2 == nil {
				return nil
			}
			return fmt.Errorf("launchctl bootout: job still loaded: %w", err)
		}
	}

	var lastOut []byte
	var lastErr error
	for i := 0; i < cfg.bootstrapTries; i++ {
		if i > 0 {
			cfg.sleep(250 * time.Millisecond)
		}
		out, err := cfg.run("bootstrap", domain, plist)
		if err == nil {
			return nil
		}
		lastOut, lastErr = out, err
		if _, printErr := cfg.run("print", target); printErr == nil {
			if _, err2 := cfg.run("kickstart", "-k", target); err2 == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("launchctl bootstrap: %w\n%s", lastErr, lastOut)
}

func waitLaunchdGone(target string, cfg launchdLoadConfig) error {
	for i := 0; i < cfg.unloadPolls; i++ {
		if _, err := cfg.run("print", target); err != nil {
			return nil
		}
		cfg.sleep(100 * time.Millisecond)
	}
	return errors.New("timed out waiting for unload")
}
