// Package provider builds the configured gen.Provider. The subpackages hold
// the adapters themselves.
package provider

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider/anthropic"
	"github.com/Conte777/autogit/internal/provider/claudecli"
	"github.com/Conte777/autogit/internal/provider/gemini"
	"github.com/Conte777/autogit/internal/provider/httpchat"
	"github.com/Conte777/autogit/internal/provider/openai"
)

var dialects = map[string]func(httpchat.Settings) httpchat.Chat{
	"anthropic": func(s httpchat.Settings) httpchat.Chat { return &anthropic.Chat{Settings: s} },
	"gemini":    func(s httpchat.Settings) httpchat.Chat { return &gemini.Chat{Settings: s} },
	"openai":    func(s httpchat.Settings) httpchat.Chat { return &openai.Chat{Settings: s} },
}

var processes = map[string]func(config.Providers) gen.Provider{
	"claude-cli": func(p config.Providers) gen.Provider {
		return &claudecli.Provider{
			Binary:    p.ClaudeCLI.Binary,
			Model:     p.ClaudeCLI.Model,
			ExtraArgs: p.ClaudeCLI.ExtraArgs,
		}
	},
}

// Names lists every provider autogit can build.
func Names() []string { return config.ProviderNames() }

// resolve merges the key and the provider's config section over the defaults
// config declares, so the two cannot disagree. This is the only place a
// transport setting is derived from the config file.
func resolve(spec config.ProviderSpec, p config.Providers, key string) (httpchat.Settings, error) {
	section, d := *spec.HTTP(&p), spec.HTTPDefaults

	s := httpchat.Settings{
		APIKey:    key,
		Model:     section.Model,
		BaseURL:   strings.TrimRight(section.BaseURL, "/"),
		MaxTokens: section.MaxTokens,
	}
	if s.Model == "" {
		s.Model = d.Model
	}
	if s.BaseURL == "" {
		s.BaseURL = d.BaseURL
	}
	if s.MaxTokens <= 0 {
		s.MaxTokens = d.MaxTokens
	}
	if s.APIKey == "" && (!spec.KeyOptionalOnCustomBaseURL || s.BaseURL == d.BaseURL) {
		return httpchat.Settings{}, fmt.Errorf("no API key: set %s", spec.EnvKey)
	}
	return s, nil
}

// Build turns the configuration into a provider.
func Build(cfg *config.Config, env func(string) (string, bool), client *http.Client) (gen.Provider, error) {
	spec, known := config.LookupProvider(cfg.Provider)
	if !known {
		return nil, fmt.Errorf("unknown provider %q; known: %v", cfg.Provider, Names())
	}

	if build, wired := processes[spec.Name]; wired {
		return build(cfg.Providers), nil
	}

	dialect, wired := dialects[spec.Name]
	if !wired {
		return nil, fmt.Errorf("provider %q is not wired up", spec.Name)
	}
	settings, err := resolve(spec, cfg.Providers, config.APIKey(spec.Name, env))
	if err != nil {
		return nil, err
	}
	return &httpchat.Provider{Chat: dialect(settings), Client: client}, nil
}
