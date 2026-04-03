package cli

import (
	"testing"
)

// ---------------------------------------------------------------------------
// suggestCommands
// ---------------------------------------------------------------------------

// seedRegistry registers a small set of predictable commands for testing.
// It returns a cleanup function that restores the previous registry state.
func seedRegistry(t *testing.T) func() {
	t.Helper()
	prev := globalRegistry
	globalRegistry = &Registry{}

	for _, cmd := range []struct {
		name, desc string
	}{
		{"services:list", "List all managed services"},
		{"services:start", "Start a stopped service"},
		{"services:stop", "Stop a running service"},
		{"services:restart", "Restart a service"},
		{"sites:list", "List all sites"},
		{"sites:get", "Show details for a site"},
		{"php:versions", "List installed PHP versions"},
		{"php:settings", "Show PHP settings"},
	} {
		cmd := cmd
		Register(&Cmd{Name: cmd.name, Description: cmd.desc})
	}

	return func() { globalRegistry = prev }
}

// TestSuggestCommands_ExactNamespace verifies that an exact namespace word
// returns all commands in that namespace.
func TestSuggestCommands_ExactNamespace(t *testing.T) {
	defer seedRegistry(t)()

	got := suggestCommands("services")
	if len(got) != 4 {
		t.Errorf("expected 4 services:* commands, got %d", len(got))
	}
	for _, cmd := range got {
		if cmd.Name[:8] != "services" {
			t.Errorf("unexpected command %q in services namespace results", cmd.Name)
		}
	}
}

// TestSuggestCommands_NamespaceWithTrailingColon verifies that "services:"
// (trailing colon stripped before matching) returns the same result as "services".
func TestSuggestCommands_NamespaceWithTrailingColon(t *testing.T) {
	defer seedRegistry(t)()

	// suggestCommands receives the query after TrimSuffix(":", "") is done by Dispatch.
	// So we test the stripped form directly.
	got := suggestCommands("services")
	if len(got) != 4 {
		t.Errorf("expected 4 suggestions for 'services', got %d", len(got))
	}
}

// TestSuggestCommands_NamespacePrefix verifies that a short prefix of a namespace
// returns all commands in the matching namespace.
func TestSuggestCommands_NamespacePrefix(t *testing.T) {
	defer seedRegistry(t)()

	got := suggestCommands("serv") // prefix of "services"
	if len(got) != 4 {
		t.Errorf("expected 4 suggestions for 'serv', got %d", len(got))
	}
}

// TestSuggestCommands_FullCommandPrefix verifies that a partial full command
// name returns only the matching commands.
func TestSuggestCommands_FullCommandPrefix(t *testing.T) {
	defer seedRegistry(t)()

	got := suggestCommands("services:s") // matches services:start, services:stop
	if len(got) != 2 {
		t.Errorf("expected 2 suggestions for 'services:s', got %d: %v", len(got), names(got))
	}
}

// TestSuggestCommands_FullCommandPrefixNarrow verifies narrowing to a single result.
func TestSuggestCommands_FullCommandPrefixNarrow(t *testing.T) {
	defer seedRegistry(t)()

	got := suggestCommands("services:re") // only services:restart
	if len(got) != 1 {
		t.Errorf("expected 1 suggestion for 'services:re', got %d: %v", len(got), names(got))
	}
	if got[0].Name != "services:restart" {
		t.Errorf("expected services:restart, got %q", got[0].Name)
	}
}

// TestSuggestCommands_NoMatch verifies that a completely unknown string
// returns an empty slice.
func TestSuggestCommands_NoMatch(t *testing.T) {
	defer seedRegistry(t)()

	got := suggestCommands("zzz")
	if len(got) != 0 {
		t.Errorf("expected no suggestions for 'zzz', got %d", len(got))
	}
}

// TestSuggestCommands_MultiNamespacePrefix verifies that a prefix matching
// multiple namespaces returns commands from all of them.
func TestSuggestCommands_MultiNamespacePrefix(t *testing.T) {
	defer seedRegistry(t)()

	// "s" prefixes both "services" and "sites"
	got := suggestCommands("s")
	if len(got) < 6 {
		t.Errorf("expected at least 6 suggestions for 's' (services+sites), got %d: %v", len(got), names(got))
	}
}

// ---------------------------------------------------------------------------
// looksLikeCLIToken
// ---------------------------------------------------------------------------

// TestLooksLikeCLIToken_ExactNamespace verifies that an exact namespace name
// is recognised as a CLI token.
func TestLooksLikeCLIToken_ExactNamespace(t *testing.T) {
	defer seedRegistry(t)()
	if !looksLikeCLIToken("services") {
		t.Error("expected 'services' to look like a CLI token")
	}
}

// TestLooksLikeCLIToken_NamespacePrefix verifies that a prefix of a namespace
// is recognised as a CLI token.
func TestLooksLikeCLIToken_NamespacePrefix(t *testing.T) {
	defer seedRegistry(t)()
	if !looksLikeCLIToken("serv") {
		t.Error("expected 'serv' to look like a CLI token")
	}
}

// TestLooksLikeCLIToken_UnknownWord verifies that an unrelated word is not
// recognised as a CLI token, so it falls through to main.go's handler.
func TestLooksLikeCLIToken_UnknownWord(t *testing.T) {
	defer seedRegistry(t)()
	if looksLikeCLIToken("foobar") {
		t.Error("expected 'foobar' NOT to look like a CLI token")
	}
}

// TestLooksLikeCLIToken_SystemSubcommand verifies that system subcommands
// like "install" are not treated as CLI tokens (they have no namespace match).
func TestLooksLikeCLIToken_SystemSubcommand(t *testing.T) {
	defer seedRegistry(t)()
	// "install" has no namespace in our seeded registry
	if looksLikeCLIToken("install") {
		t.Error("expected 'install' NOT to look like a CLI token")
	}
}

// ---------------------------------------------------------------------------
// services command registration
// ---------------------------------------------------------------------------

// TestServicesAvailable_IsRegistered verifies that the services:available
// command is registered in the global registry.
func TestServicesAvailable_IsRegistered(t *testing.T) {
	cmd := Find("services:available")
	if cmd == nil {
		t.Fatal("services:available: command is not registered")
	}
	if cmd.Name != "services:available" {
		t.Errorf("services:available: Name = %q, want %q", cmd.Name, "services:available")
	}
	if cmd.Description == "" {
		t.Error("services:available: Description is empty")
	}
	if cmd.Handler == nil {
		t.Error("services:available: Handler is nil")
	}
}

// TestServicesInstall_IsRegistered verifies that the services:install
// command is registered in the global registry with the expected metadata.
func TestServicesInstall_IsRegistered(t *testing.T) {
	cmd := Find("services:install")
	if cmd == nil {
		t.Fatal("services:install: command is not registered")
	}
	if cmd.Name != "services:install" {
		t.Errorf("services:install: Name = %q, want %q", cmd.Name, "services:install")
	}
	if cmd.Description == "" {
		t.Error("services:install: Description is empty")
	}
	if cmd.Handler == nil {
		t.Error("services:install: Handler is nil")
	}
	// Should declare a service-id argument.
	if len(cmd.Args) == 0 {
		t.Error("services:install: Args is empty — expected at least one ArgDef")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func names(cmds []*Cmd) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
