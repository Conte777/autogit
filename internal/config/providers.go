package config

import "slices"

// ProviderSpec is the single declaration of one provider: where its settings
// live, which variable carries its key, and what it falls back to.
type ProviderSpec struct {
	Name                       string
	EnvKey                     string
	HTTP                       func(*Providers) *HTTPProvider
	HTTPDefaults               HTTPProvider
	KeyOptionalOnCustomBaseURL bool
	Model                      func(*Providers) *string

	defaults func(*Providers)
}

var providerSpecs = []ProviderSpec{
	{
		Name:   "anthropic",
		EnvKey: "ANTHROPIC_API_KEY",
		HTTP:   func(p *Providers) *HTTPProvider { return &p.Anthropic },
		HTTPDefaults: HTTPProvider{
			Model:     "claude-haiku-4-5",
			BaseURL:   "https://api.anthropic.com/v1",
			MaxTokens: 1024,
		},
		Model: func(p *Providers) *string { return &p.Anthropic.Model },
	},
	{
		Name:  "claude-cli",
		Model: func(p *Providers) *string { return &p.ClaudeCLI.Model },
		defaults: func(p *Providers) {
			p.ClaudeCLI = ClaudeCLI{Binary: "claude", Model: "haiku"}
		},
	},
	{
		Name:   "gemini",
		EnvKey: "GEMINI_API_KEY",
		HTTP:   func(p *Providers) *HTTPProvider { return &p.Gemini },
		HTTPDefaults: HTTPProvider{
			Model:     "gemini-2.5-flash",
			BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
			MaxTokens: 1024,
		},
		Model: func(p *Providers) *string { return &p.Gemini.Model },
	},
	{
		Name:   "openai",
		EnvKey: "OPENAI_API_KEY",
		HTTP:   func(p *Providers) *HTTPProvider { return &p.OpenAI },
		HTTPDefaults: HTTPProvider{
			Model:     "gpt-4.1-mini",
			BaseURL:   "https://api.openai.com/v1",
			MaxTokens: 1024,
		},
		// Ollama, LM Studio and the other compatible servers speak this
		// dialect without a key; api.openai.com does not.
		KeyOptionalOnCustomBaseURL: true,
		Model:                      func(p *Providers) *string { return &p.OpenAI.Model },
	},
}

// ProviderSpecs lists every provider autogit knows, in a stable order.
func ProviderSpecs() []ProviderSpec { return slices.Clone(providerSpecs) }

// ProviderNames lists the names of every provider autogit knows.
func ProviderNames() []string {
	names := make([]string, 0, len(providerSpecs))
	for _, s := range providerSpecs {
		names = append(names, s.Name)
	}
	return names
}

// LookupProvider finds a provider by name.
func LookupProvider(name string) (ProviderSpec, bool) {
	for _, s := range providerSpecs {
		if s.Name == name {
			return s, true
		}
	}
	return ProviderSpec{}, false
}

func defaultProviders() Providers {
	var p Providers
	for _, s := range providerSpecs {
		if s.HTTP != nil {
			*s.HTTP(&p) = s.HTTPDefaults
			continue
		}
		s.defaults(&p)
	}
	return p
}
