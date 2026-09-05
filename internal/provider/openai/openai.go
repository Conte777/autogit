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

	"github.com/Conte777/autogit/internal/provider/httpchat"
)

// Chat is the Chat Completions dialect.
type Chat struct{ httpchat.Settings }

func (c *Chat) Name() string { return "openai" }

func (c *Chat) Request(ctx context.Context, system string, history []httpchat.Message) (*http.Request, error) {
	messages := make([]map[string]string, 0, len(history)+1)
	messages = append(messages, map[string]string{"role": "system", "content": system})
	for _, m := range history {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Text})
	}
	payload := map[string]any{"model": c.Model, "messages": messages}
	if c.MaxTokens > 0 {
		payload["max_completion_tokens"] = c.MaxTokens
	}

	headers := map[string]string{}
	if c.APIKey != "" {
		headers["Authorization"] = "Bearer " + c.APIKey
	}
	return httpchat.JSONRequest(ctx, c.BaseURL+"/chat/completions", payload, headers)
}

func (c *Chat) Reply(body []byte) (string, error) {
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
