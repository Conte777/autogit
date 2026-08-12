// Package openai talks to the Chat Completions API. The request is kept
// deliberately plain so that Ollama, LM Studio and other compatible servers
// work through the same adapter with a different baseUrl.
package openai

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
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4.1-mini"
)

// Config is the provider section plus the key from the environment.
type Config struct {
	APIKey    string
	Model     string
	BaseURL   string
	MaxTokens int
}

// New builds a provider.
func New(cfg Config, client *http.Client) (gen.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	// A local server needs no key; api.openai.com does.
	if cfg.APIKey == "" && cfg.BaseURL == defaultBaseURL {
		return nil, errors.New("no API key: set OPENAI_API_KEY")
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	return &httpchat.Provider{Chat: &chat{cfg: cfg}, Client: client}, nil
}

type chat struct{ cfg Config }

func (c *chat) Name() string { return "openai" }

func (c *chat) Request(ctx context.Context, system string, history []httpchat.Message) (*http.Request, error) {
	messages := make([]map[string]string, 0, len(history)+1)
	messages = append(messages, map[string]string{"role": "system", "content": system})
	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Text})
	}
	payload := map[string]any{"model": c.cfg.Model, "messages": messages}
	if c.cfg.MaxTokens > 0 {
		payload["max_completion_tokens"] = c.cfg.MaxTokens
	}

	headers := map[string]string{}
	if c.cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + c.cfg.APIKey
	}
	return httpchat.JSONRequest(ctx, c.cfg.BaseURL+"/chat/completions", payload, headers)
}

func (c *chat) Reply(body []byte) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("openai: cannot parse response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openai: response carried no choices")
	}
	return resp.Choices[0].Message.Content, nil
}
