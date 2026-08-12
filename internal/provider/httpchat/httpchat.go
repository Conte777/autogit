// Package httpchat is the shared plumbing of the HTTP providers: a session that
// replays its accumulated history on every turn, plus retry and error mapping.
package httpchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Conte777/autogit/internal/gen"
)

// Message is one turn of the conversation.
type Message struct {
	Role string // "user" or "assistant"
	Text string
}

// Chat is the provider-specific half: build a request, read a reply.
type Chat interface {
	Name() string
	Request(ctx context.Context, system string, history []Message) (*http.Request, error)
	Reply(body []byte) (string, error)
}

// Provider adapts a Chat into a gen.Provider.
type Provider struct {
	Chat       Chat
	Client     *http.Client
	MaxRetries int
}

func (p *Provider) Name() string { return p.Chat.Name() }

// Start opens a session. No process is involved: the whole history is resent
// on every turn.
func (p *Provider) Start(_ context.Context, system string) (gen.Session, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	retries := p.MaxRetries
	if retries <= 0 {
		retries = 2
	}
	return &session{chat: p.Chat, client: client, system: system, retries: retries}, nil
}

type session struct {
	chat    Chat
	client  *http.Client
	system  string
	retries int
	history []Message
}

func (s *session) Send(ctx context.Context, text string) (string, error) {
	s.history = append(s.history, Message{Role: "user", Text: text})

	body, err := s.post(ctx)
	if err != nil {
		return "", err
	}
	reply, err := s.chat.Reply(body)
	if err != nil {
		return "", err
	}
	s.history = append(s.history, Message{Role: "assistant", Text: reply})
	return reply, nil
}

func (s *session) Close() error { return nil }

// post retries transient failures here rather than in gen.Generate: a retried
// HTTP call is transport business, and the generation loop must stay terminal
// on provider errors.
func (s *session) post(ctx context.Context) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= s.retries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := s.chat.Request(ctx, s.system, s.history)
		if err != nil {
			return nil, err
		}
		body, retryable, err := do(s.client, req)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}
	return nil, lastErr
}

func do(client *http.Client, req *http.Request) (body []byte, retryable bool, err error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded), err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, true, err
	}
	if resp.StatusCode == http.StatusOK {
		return body, false, nil
	}

	apiErr := fmt.Errorf("%s returned %s: %s", req.URL.Host, resp.Status, snippet(body))
	// 408/429/5xx are worth another go; 401 and 400 never are.
	retryable = resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= 500
	return nil, retryable, apiErr
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

// JSONRequest builds a POST with a JSON body and the given headers.
func JSONRequest(ctx context.Context, url string, payload any, headers map[string]string) (*http.Request, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}
