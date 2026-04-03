package cli

import (
	"fmt"
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
		Description: "Show current PHP ini settings (applies to all versions)",
		Examples:    []string{"devctl php:settings", "devctl php:settings --json"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			s, err := c.GetPHPSettings()
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(s)
				return nil
			}
			Header("PHP Settings (all versions)")
			KV("memory_limit", s.MemoryLimit)
			KV("upload_max_filesize", s.UploadMaxFilesize)
			KV("post_max_size", s.PostMaxSize)
			KV("max_execution_time", s.MaxExecutionTime)
			fmt.Println()
			fmt.Println(styleDim.Render("Run `devctl services:restart php-fpm-<version>` after changes."))
			return nil
		},
	})

	Register(&Cmd{
		Name:        "php:set",
		Description: "Update PHP ini settings",
		Usage:       "<key=value>...",
		Args:        []ArgDef{{Name: "key=value", Description: "One or more settings to update (memory_limit, upload_max_filesize, post_max_size, max_execution_time)"}},
		Examples: []string{
			"devctl php:set memory_limit=512M",
			"devctl php:set memory_limit=512M upload_max_filesize=128M",
			"devctl php:set max_execution_time=120",
		},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: devctl php:set <key=value>...\n  keys: memory_limit, upload_max_filesize, post_max_size, max_execution_time")
			}

			// Parse key=value pairs
			kv := parseKV(args)

			// Load current settings as defaults
			current, err := c.GetPHPSettings()
			if err != nil {
				return fmt.Errorf("read current settings: %w", err)
			}

			updated := PHPSettings{
				MemoryLimit:       getKV(kv, "memory_limit", current.MemoryLimit),
				UploadMaxFilesize: getKV(kv, "upload_max_filesize", current.UploadMaxFilesize),
				PostMaxSize:       getKV(kv, "post_max_size", current.PostMaxSize),
				MaxExecutionTime:  getKV(kv, "max_execution_time", current.MaxExecutionTime),
			}

			result, err := c.SetPHPSettings(updated)
			if err != nil {
				return err
			}
			if jsonMode {
				PrintJSON(result)
				return nil
			}
			PrintOK("PHP settings updated")
			KV("memory_limit", result.MemoryLimit)
			KV("upload_max_filesize", result.UploadMaxFilesize)
			KV("post_max_size", result.PostMaxSize)
			KV("max_execution_time", result.MaxExecutionTime)
			fmt.Println()
			fmt.Println(styleDim.Render("Restart PHP-FPM to apply: devctl services:restart php-fpm-<version>"))
			return nil
		},
	})
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

// getKV returns m[key] if present, otherwise def.
func getKV(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}
