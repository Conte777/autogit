package config_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/Conte777/autogit/internal/config"
)

// A guard on Default() still being built from the table, and on the table
// being filled in at all. That the defaults actually reach a provider is a
// different claim, proved in internal/provider by resolving a blank section.
func TestDefaultConfigCarriesTheDeclaredProviderDefaults(t *testing.T) {
	defaults := config.Default().Providers
	for _, spec := range config.ProviderSpecs() {
		if spec.HTTP == nil {
			continue
		}
		if got := *spec.HTTP(&defaults); got != spec.HTTPDefaults {
			t.Errorf("%s: Default() has %+v, the spec declares %+v", spec.Name, got, spec.HTTPDefaults)
		}
		if spec.HTTPDefaults.BaseURL == "" {
			t.Errorf("%s: no default baseUrl", spec.Name)
		}
		if spec.HTTPDefaults.Model == "" {
			t.Errorf("%s: no default model", spec.Name)
		}
		if spec.EnvKey == "" {
			t.Errorf("%s: an HTTP provider with no key variable", spec.Name)
		}
	}
}

// Every spec is dereferenced without a nil check, so a half-filled entry
// panics inside Load rather than reporting a config error.
func TestEveryProviderSpecIsComplete(t *testing.T) {
	for _, spec := range config.ProviderSpecs() {
		if spec.Name == "" {
			t.Error("a provider spec has no name")
		}
		if spec.Model == nil {
			t.Errorf("%s: no Model accessor", spec.Name)
		}
		if spec.KeyOptionalOnCustomBaseURL && spec.HTTP == nil {
			t.Errorf("%s: a base URL rule on a provider with no base URL", spec.Name)
		}
	}
	// Default() runs the defaults of every spec, including the non-HTTP ones
	// whose accessor is unexported.
	if got := config.Default().Providers.ClaudeCLI.Binary; got == "" {
		t.Errorf("claude-cli binary = %q after Default()", got)
	}
}

// The schema is what an editor validates a config against, so a provider the
// table declares must be spellable there and one it does not must not be.
func TestSchemaEnumeratesTheProviderTable(t *testing.T) {
	raw, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			Provider struct {
				Enum []string `json:"enum"`
			} `json:"provider"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(doc.Properties.Provider.Enum, config.ProviderNames()) {
		t.Errorf("schema enum = %v, want %v", doc.Properties.Provider.Enum, config.ProviderNames())
	}
}

func TestProviderNamesAreUniqueAndSorted(t *testing.T) {
	names := config.ProviderNames()
	if len(names) == 0 {
		t.Fatal("no providers declared")
	}
	if !slices.IsSorted(names) {
		t.Errorf("ProviderNames() = %v, want a stable sorted order", names)
	}
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Errorf("ProviderNames() = %v, want no duplicates", names)
	}
	for _, name := range names {
		if _, ok := config.LookupProvider(name); !ok {
			t.Errorf("LookupProvider(%q) found nothing", name)
		}
	}
	if _, ok := config.LookupProvider("nope"); ok {
		t.Error("LookupProvider found an undeclared provider")
	}
}

// Every provider's Model accessor must reach its own section, or a --model
// flag would silently rewrite a neighbour's.
func TestSetModelIsScopedToTheSelectedProvider(t *testing.T) {
	for _, name := range config.ProviderNames() {
		cfg := config.Default()
		cfg.Provider = name
		cfg.SetModel("sentinel")

		if got := cfg.Model(); got != "sentinel" {
			t.Errorf("%s: Model() = %q after SetModel", name, got)
		}
		for _, other := range config.ProviderNames() {
			if other == name {
				continue
			}
			probe := cfg
			probe.Provider = other
			if probe.Model() == "sentinel" {
				t.Errorf("SetModel on %s leaked into %s", name, other)
			}
		}
	}

	cfg := config.Default()
	cfg.Provider = "nope"
	cfg.SetModel("sentinel")
	if got := cfg.Model(); got != "" {
		t.Errorf("Model() = %q for an unknown provider", got)
	}
}
