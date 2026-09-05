package provider

import (
	"slices"
	"testing"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/provider/httpchat"
)

// A dialect keyed on a name config does not declare is unreachable, and a
// declared name in neither map fails at run time instead of at build time.
func TestEveryDeclaredProviderIsWiredExactlyOnce(t *testing.T) {
	names := config.ProviderNames()

	for name := range dialects {
		if !slices.Contains(names, name) {
			t.Errorf("dialects has %q, which config does not declare", name)
		}
		if _, both := subprocesses[name]; both {
			t.Errorf("%q is wired as both a dialect and a subprocess", name)
		}
		// resolve dereferences spec.HTTP; without one, Build panics.
		if spec, ok := config.LookupProvider(name); ok && spec.HTTP == nil {
			t.Errorf("dialect %q has no HTTP section in its spec", name)
		}
	}
	for name := range subprocesses {
		if !slices.Contains(names, name) {
			t.Errorf("subprocesses has %q, which config does not declare", name)
		}
	}

	for _, name := range names {
		_, isDialect := dialects[name]
		_, isSubprocess := subprocesses[name]
		if !isDialect && !isSubprocess {
			t.Errorf("config declares %q but nothing builds it", name)
		}
	}
}

// A dialect must answer to the name it is keyed on, or doctor and the error
// messages would name a different provider than the one talking.
func TestDialectNamesMatchTheirKey(t *testing.T) {
	for name, build := range dialects {
		if got := build(httpchat.Settings{}).Name(); got != name {
			t.Errorf("dialects[%q] is named %q", name, got)
		}
	}
}
