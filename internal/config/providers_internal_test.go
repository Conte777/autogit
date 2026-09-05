package config

import "testing"

// defaultProviders dereferences one of the two without checking, so a spec
// carrying neither panics inside Load rather than reporting a config error.
func TestEverySpecDeclaresExactlyOneDefaultsMechanism(t *testing.T) {
	for _, spec := range providerSpecs {
		http, closure := spec.HTTP != nil, spec.defaults != nil
		if http == closure {
			t.Errorf("%s: HTTP=%v defaults=%v, want exactly one", spec.Name, http, closure)
		}
	}
}
