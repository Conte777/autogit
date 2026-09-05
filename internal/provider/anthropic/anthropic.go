// Package anthropic talks to the Messages API with the user's own API key.
// It is the escape hatch from claude-cli: `-p` has drifted away from
// subscriptions twice already, and this path must be tested, not theoretical.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Conte777/autogit/internal/provider/httpchat"
)

const apiVersion = "2023-06-01"

// Chat is the Messages API dialect.
type Chat struct{ httpchat.Settings }

func (c *Chat) Name() string { return "anthropic" }

func (c *Chat) Request(ctx context.Context, system string, history []httpchat.Message) (*http.Request, error) {
	messages := make([]map[string]any, 0, len(history))
	for _, m := range history {
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Text})
	}
	payload := map[string]any{
		"model":      c.Model,
		"max_tokens": c.MaxTokens,
		"system":     system,
		"messages":   messages,
	}
	return httpchat.JSONRequest(ctx, c.BaseURL+"/messages", payload, map[string]string{
		"x-api-key":         c.APIKey,
		"anthropic-version": apiVersion,
	})
}

func (c *Chat) Reply(body []byte) (string, error) {
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
