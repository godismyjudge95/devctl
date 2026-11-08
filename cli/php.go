package cli

import (
	"fmt"
	"slices"
	"strings"
)

func init() {
	Register(&Cmd{
		Name:        "php:versions",
		Description: "List installed PHP versions and their FPM status",
		Examples:    []string{"devctl php:versions", "devctl php:versions --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			versions, err := c.ListPHPVersions()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(versions)
				return nil
			}
			if len(versions) == 0 {
				fmt.Println("No PHP versions installed.")
				return nil
			}
			var rows [][]string
			for _, v := range versions {
				rows = append(rows, []string{
					"PHP " + v.Version,
					StatusStyle(v.Status),
					styleDim.Render(v.FPMSocket),
				})
			}
			Table([]string{"Version", "Status", "FPM Socket"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:settings",
		Description: "Show PHP ini settings for all versions or one version",
		Usage:       "[all|<version>] [--version=<ver>]",
		Args:        []ArgDef{{Name: "version", Description: "PHP version (e.g. 8.4). Default: all versions", Optional: true}},
		Flags:       []FlagDef{{Name: "version", Description: "Same as the version argument"}},
		Examples:    []string{"devctl php:settings", "devctl php:settings 8.4", "devctl php:settings --version=8.4 --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			pos, ver, j, err := splitPHPArgs(args, true)
			if err != nil {
				return err
			}
			if len(pos) > 0 {
				if ver != "" && ver != pos[0] {
					return fmt.Errorf("give the version once: %s or --version=%s", pos[0], ver)
				}
				ver = pos[0]
			}
			if ver == "all" {
				ver = ""
			}
			s, err := c.GetPHPSettings(ver)
			if err != nil {
				return err
			}
			if jsonMode || j {
				PrintJSON(s)
				return nil
			}
			printPHPSettings(s)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:set",
		Description: "Update PHP ini settings for all versions or one version",
		Usage:       "<key=value>... [--version=<ver>]",
		Args:        []ArgDef{{Name: "key=value", Description: "One or more settings to update (" + strings.Join(phpSettingKeys, ", ") + ")"}},
		Flags:       []FlagDef{{Name: "version", Description: "Only change this PHP version (e.g. 8.4). Default: all versions"}},
		Examples: []string{
			"devctl php:set memory_limit=512M",
			"devctl php:set memory_limit=512M upload_max_filesize=128M",
			"devctl php:set memory_limit=1G --version=8.4",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			pos, ver, j, err := splitPHPArgs(args, true)
			if err != nil {
				return err
			}
			kv := parseKV(pos)
			if len(kv) == 0 {
				return fmt.Errorf("usage: devctl php:set <key=value>... [--version=<ver>]\n  keys: %s", strings.Join(phpSettingKeys, ", "))
			}
			for k := range kv {
				if !slices.Contains(phpSettingKeys, k) {
					return fmt.Errorf("unknown setting %q (keys: %s). Use devctl php:ini:set for other php.ini directives", k, strings.Join(phpSettingKeys, ", "))
				}
			}
			result, err := c.SetPHPSettings(ver, kv)
			if err != nil {
				return err
			}
			if jsonMode || j {
				PrintJSON(result)
				return nil
			}
			PrintOK("PHP settings updated. Running PHP-FPM pools were restarted.")
			printPHPSettings(result)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:ini:list",
		Description: "List the php.ini directives devctl manages",
		Usage:       "[all|<version>]",
		Args:        []ArgDef{{Name: "scope", Description: "Only show directives for all versions (all) or one version (e.g. 8.4)", Optional: true}},
		Examples:    []string{"devctl php:ini:list", "devctl php:ini:list 8.4", "devctl php:ini:list --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			pos, _, j, err := splitPHPArgs(args, false)
			if err != nil {
				return err
			}
			scope := ""
			if len(pos) > 0 {
				scope = pos[0]
			}
			ds, err := c.ListPHPIni(scope)
			if err != nil {
				return err
			}
			if jsonMode || j {
				PrintJSON(ds)
				return nil
			}
			if len(ds) == 0 {
				fmt.Println("No php.ini directives set by devctl.")
				return nil
			}
			var rows [][]string
			for _, d := range ds {
				rows = append(rows, []string{d.Scope, d.Key, d.Value})
			}
			Table([]string{"Scope", "Key", "Value"}, rows)
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:ini:set",
		Description: "Set a php.ini directive for all versions or one version",
		Usage:       "<all|version> <key> <value>",
		Args: []ArgDef{
			{Name: "scope", Description: "all, or a PHP version (e.g. 8.4). A version value wins over the all value"},
			{Name: "key", Description: "php.ini directive (e.g. display_errors, opcache.jit)"},
			{Name: "value", Description: "Value written as is. Quote values that contain spaces or ini operators"},
		},
		Examples: []string{
			"devctl php:ini:set all display_errors On",
			"devctl php:ini:set 8.4 opcache.jit tracing",
			"devctl php:ini:set all error_reporting 'E_ALL & ~E_DEPRECATED'",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			pos, _, j, err := splitPHPArgs(args, false)
			if err != nil {
				return err
			}
			if len(pos) < 3 {
				return fmt.Errorf("usage: devctl php:ini:set <all|version> <key> <value>")
			}
			d, err := c.SetPHPIni(pos[0], pos[1], strings.Join(pos[2:], " "))
			if err != nil {
				return err
			}
			if jsonMode || j {
				PrintJSON(d)
				return nil
			}
			PrintOK(fmt.Sprintf("%s = %s (%s). Running PHP-FPM pools were restarted.", d.Key, d.Value, d.Scope))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:ini:unset",
		Description: "Remove a php.ini directive that devctl manages",
		Usage:       "<all|version> <key>",
		Args: []ArgDef{
			{Name: "scope", Description: "all, or a PHP version (e.g. 8.4)"},
			{Name: "key", Description: "php.ini directive to remove"},
		},
		Examples: []string{"devctl php:ini:unset all display_errors", "devctl php:ini:unset 8.4 memory_limit"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			pos, _, j, err := splitPHPArgs(args, false)
			if err != nil {
				return err
			}
			if len(pos) != 2 {
				return fmt.Errorf("usage: devctl php:ini:unset <all|version> <key>")
			}
			if err := c.UnsetPHPIni(pos[0], pos[1]); err != nil {
				return err
			}
			if jsonMode || j {
				PrintJSON(map[string]string{"status": "ok", "scope": pos[0], "key": pos[1]})
				return nil
			}
			PrintOK(fmt.Sprintf("Removed %s (%s). Running PHP-FPM pools were restarted.", pos[1], pos[0]))
			return nil
		},
	})
}

var phpSettingKeys = []string{"memory_limit", "upload_max_filesize", "post_max_size", "max_execution_time"}

func printPHPSettings(s PHPSettings) {
	if s.Scope == "" || s.Scope == "all" {
		Header("PHP Settings (all versions)")
	} else {
		Header("PHP Settings (PHP " + s.Scope + ")")
	}
	values := map[string]string{
		"memory_limit":        s.MemoryLimit,
		"upload_max_filesize": s.UploadMaxFilesize,
		"post_max_size":       s.PostMaxSize,
		"max_execution_time":  s.MaxExecutionTime,
	}
	for _, k := range phpSettingKeys {
		v := values[k]
		if s.Scope != "all" && slices.Contains(s.Overrides, k) {
			v += styleDim.Render("  (set for this version)")
		}
		KV(k, v)
	}
}

// splitPHPArgs takes --version and --json out of args. Flags may come before
// or after positionals, and "--" ends flags. Only "--" prefixes count as
// flags, so values such as -1 stay positional.
func splitPHPArgs(args []string, withVersion bool) (pos []string, version string, jsonFlag bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(pos, args[i+1:]...), version, jsonFlag, nil
		case a == "--json":
			jsonFlag = true
		case withVersion && strings.HasPrefix(a, "--version="):
			version = strings.TrimPrefix(a, "--version=")
		case withVersion && a == "--version":
			if i+1 >= len(args) {
				return nil, "", false, fmt.Errorf("--version needs a value")
			}
			i++
			version = args[i]
		case strings.HasPrefix(a, "--"):
			return nil, "", false, fmt.Errorf("unknown flag %s", a)
		default:
			pos = append(pos, a)
		}
	}
	return pos, version, jsonFlag, nil
}

// parseKV parses a slice of "key=value" strings into a map.
func parseKV(args []string) map[string]string {
	m := map[string]string{}
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
