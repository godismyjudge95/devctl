package elevate

import (
	"fmt"
	"strings"
)

func helperAptInstall(args []string) error {
	flags, positional, err := parseFlags(args)
	if err != nil {
		return err
	}
	_ = flags
	// Packages after optional "--" separator.
	pkgs := positional
	if len(pkgs) == 0 {
		return fmt.Errorf("apt-install: no packages specified")
	}
	for _, p := range pkgs {
		if strings.HasPrefix(p, "-") || strings.ContainsAny(p, "/ \t\n") {
			return fmt.Errorf("apt-install: invalid package name %q", p)
		}
		if _, ok := allowedAptPackages[p]; !ok {
			return fmt.Errorf("apt-install: package %q is not allowlisted", p)
		}
	}
	if err := runPinned("apt-get", "update"); err != nil {
		return err
	}
	installArgs := append([]string{"install", "-y", "--no-install-recommends"}, pkgs...)
	if err := runPinned("apt-get", installArgs...); err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}
