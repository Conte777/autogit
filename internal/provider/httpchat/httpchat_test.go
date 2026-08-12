package httpchat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Conte777/autogit/internal/provider/anthropic"
)

type capturedBody struct {
	Model    string `json:"model"`
	System   string `json:"system"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func TestSessionReplaysHistory(t *testing.T) {
	var bodies []capturedBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b capturedBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, b)
		if got := r.Header.Get("x-api-key"); got != "sk-test" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header is missing")
		}
		reply := "reply " + string(rune('0'+len(bodies)))
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"` + reply + `"}]}`))
	}))
	defer srv.Close()

	p, err := anthropic.New(anthropic.Config{APIKey: "sk-test", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.Start(context.Background(), "SYS")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if len(bodies[0].Messages) != 1 {
		t.Errorf("first request carried %d messages, want 1", len(bodies[0].Messages))
	}
	if len(bodies[1].Messages) != 3 {
		t.Fatalf("second request carried %d messages, want user/assistant/user", len(bodies[1].Messages))
	}
	if bodies[1].Messages[1].Role != "assistant" {
		t.Errorf("history lost the model's own turn: %+v", bodies[1].Messages)
	}
	if bodies[1].System != "SYS" {
		t.Errorf("system = %q, want it resent every turn", bodies[1].System)
	}
}

func TestRetriesOn503AndGivesUpOn401(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	p, _ := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}, srv.Client())
	s, _ := p.Start(context.Background(), "sys")
	got, err := s.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("a 503 was not retried: %v", err)
	}
	if got != "ok" || calls.Load() != 2 {
		t.Errorf("got %q after %d calls", got, calls.Load())
	}

	var authCalls atomic.Int32
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		authCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
	}))
	defer auth.Close()

	p, _ = anthropic.New(anthropic.Config{APIKey: "bad", BaseURL: auth.URL}, auth.Client())
	s, _ = p.Start(context.Background(), "sys")
	_, err = s.Send(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("err = %v, want the API message surfaced", err)
	}
	if authCalls.Load() != 1 {
		t.Errorf("a 401 was retried %d times, want none", authCalls.Load()-1)
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := anthropic.New(anthropic.Config{}, nil); err == nil {
		t.Fatal("New succeeded without an API key")
	}
}
