// Package provider builds the configured gen.Provider. The subpackages hold
// the adapters themselves.
package provider

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider/anthropic"
	"github.com/Conte777/autogit/internal/provider/claudecli"
	"github.com/Conte777/autogit/internal/provider/gemini"
	"github.com/Conte777/autogit/internal/provider/openai"
)

// Names lists every provider autogit can build.
func Names() []string { return []string{"anthropic", "claude-cli", "gemini", "openai"} }

// Build turns the configuration into a provider.
func Build(cfg *config.Config, env func(string) (string, bool), client *http.Client) (gen.Provider, error) {
	key := config.APIKey(cfg.Provider, env)
	p := cfg.Providers

	switch cfg.Provider {
	case "claude-cli":
		return &claudecli.Provider{
			Binary:    p.ClaudeCLI.Binary,
			Model:     p.ClaudeCLI.Model,
			ExtraArgs: p.ClaudeCLI.ExtraArgs,
		}, nil
	case "anthropic":
		return anthropic.New(anthropic.Config{
			APIKey: key, Model: p.Anthropic.Model,
			BaseURL: p.Anthropic.BaseURL, MaxTokens: p.Anthropic.MaxTokens,
		}, client)
	case "openai":
		return openai.New(openai.Config{
			APIKey: key, Model: p.OpenAI.Model,
			BaseURL: p.OpenAI.BaseURL, MaxTokens: p.OpenAI.MaxTokens,
		}, client)
	case "gemini":
		return gemini.New(gemini.Config{
			APIKey: key, Model: p.Gemini.Model,
			BaseURL: p.Gemini.BaseURL, MaxTokens: p.Gemini.MaxTokens,
		}, client)
	}

	if slices.Contains(Names(), cfg.Provider) {
		return nil, fmt.Errorf("provider %q is not wired up", cfg.Provider)
	}
	return nil, fmt.Errorf("unknown provider %q; known: %v", cfg.Provider, Names())
}
