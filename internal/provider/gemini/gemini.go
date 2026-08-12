// Package gemini talks to the Generative Language API.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider/httpchat"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultModel   = "gemini-2.5-flash"
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
	if cfg.APIKey == "" {
		return nil, errors.New("no API key: set GEMINI_API_KEY")
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &httpchat.Provider{Chat: &chat{cfg: cfg}, Client: client}, nil
}

type chat struct{ cfg Config }

func (c *chat) Name() string { return "gemini" }

func (c *chat) Request(ctx context.Context, system string, history []httpchat.Message) (*http.Request, error) {
	contents := make([]map[string]any, 0, len(history))
	for _, m := range history {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Text}},
		})
	}
	payload := map[string]any{
		"system_instruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":           contents,
	}
	if c.cfg.MaxTokens > 0 {
		payload["generationConfig"] = map[string]any{"maxOutputTokens": c.cfg.MaxTokens}
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent", strings.TrimRight(c.cfg.BaseURL, "/"),
		url.PathEscape(c.cfg.Model))
	// The key goes in a header, not the query string: a URL ends up in logs.
	return httpchat.JSONRequest(ctx, endpoint, payload, map[string]string{"x-goog-api-key": c.cfg.APIKey})
}

func (c *chat) Reply(body []byte) (string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("gemini: cannot parse response: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return "", errors.New("gemini: response carried no candidates")
	}
	var b strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		b.WriteString(part.Text)
	}
	return b.String(), nil
}
