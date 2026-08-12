// Package anthropic talks to the Messages API with the user's own API key.
// It is the escape hatch from claude-cli: `-p` has drifted away from
// subscriptions twice already, and this path must be tested, not theoretical.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider/httpchat"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	defaultModel   = "claude-haiku-4-5"
	apiVersion     = "2023-06-01"
)

// Config is the provider section of the config file plus the key from env.
type Config struct {
	APIKey    string
	Model     string
	BaseURL   string
	MaxTokens int
}

// New builds a provider, or fails when there is no key to use.
func New(cfg Config, client *http.Client) (gen.Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("no API key: set ANTHROPIC_API_KEY")
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	return &httpchat.Provider{Chat: &chat{cfg: cfg}, Client: client}, nil
}

type chat struct{ cfg Config }

func (c *chat) Name() string { return "anthropic" }

func (c *chat) Request(ctx context.Context, system string, history []httpchat.Message) (*http.Request, error) {
	messages := make([]map[string]any, 0, len(history))
	for _, m := range history {
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Text})
	}
	payload := map[string]any{
		"model":      c.cfg.Model,
		"max_tokens": c.cfg.MaxTokens,
		"system":     system,
		"messages":   messages,
	}
	return httpchat.JSONRequest(ctx, c.cfg.BaseURL+"/messages", payload, map[string]string{
		"x-api-key":         c.cfg.APIKey,
		"anthropic-version": apiVersion,
	})
}

func (c *chat) Reply(body []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("anthropic: cannot parse response: %w", err)
	}
	var text string
	for _, block := range resp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text, nil
}
