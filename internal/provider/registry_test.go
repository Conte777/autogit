package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/provider"
)

func envOf(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

// configFor points a provider at a test server, leaving every other setting at
// its built-in default.
func configFor(t *testing.T, name, baseURL string) *config.Config {
	t.Helper()
	spec, ok := config.LookupProvider(name)
	if !ok {
		t.Fatalf("no spec for %q", name)
	}
	cfg := config.Default()
	cfg.Provider = name
	if spec.HTTP != nil {
		spec.HTTP(&cfg.Providers).BaseURL = baseURL
	}
	return &cfg
}

type dialectCase struct {
	name     string
	envKey   string
	wantPath string
	headers  map[string]string
	reply    string
	check    func(t *testing.T, payload map[string]any)
}

func TestDialects(t *testing.T) {
	cases := []dialectCase{
		{
			name:     "anthropic",
			envKey:   "ANTHROPIC_API_KEY",
			wantPath: "/messages",
			headers:  map[string]string{"x-api-key": "the-key", "anthropic-version": "2023-06-01"},
			reply:    `{"content":[{"type":"text","text":"hi"},{"type":"thinking","text":"ignored"}]}`,
			check: func(t *testing.T, payload map[string]any) {
				if payload["model"] != "claude-haiku-4-5" {
					t.Errorf("model = %v", payload["model"])
				}
				if payload["system"] != "SYS" {
					t.Errorf("system = %v, want a top-level field", payload["system"])
				}
				if payload["max_tokens"] != float64(1024) {
					t.Errorf("max_tokens = %v", payload["max_tokens"])
				}
				msgs := payload["messages"].([]any)
				if len(msgs) != 1 {
					t.Fatalf("messages = %v, want the user turn alone", msgs)
				}
				first := msgs[0].(map[string]any)
				if first["role"] != "user" || first["content"] != "ask" {
					t.Errorf("messages[0] = %v", first)
				}
			},
		},
		{
			name:     "openai",
			envKey:   "OPENAI_API_KEY",
			wantPath: "/chat/completions",
			headers:  map[string]string{"Authorization": "Bearer the-key"},
			reply:    `{"choices":[{"message":{"content":"hi"}}]}`,
			check: func(t *testing.T, payload map[string]any) {
				if payload["model"] != "gpt-4.1-mini" {
					t.Errorf("model = %v", payload["model"])
				}
				if payload["max_completion_tokens"] != float64(1024) {
					t.Errorf("max_completion_tokens = %v", payload["max_completion_tokens"])
				}
				msgs := payload["messages"].([]any)
				if len(msgs) != 2 {
					t.Fatalf("messages = %v, want system then user", msgs)
				}
				sys := msgs[0].(map[string]any)
				if sys["role"] != "system" || sys["content"] != "SYS" {
					t.Errorf("messages[0] = %v, want the system prompt as a turn", sys)
				}
			},
		},
		{
			name:     "gemini",
			envKey:   "GEMINI_API_KEY",
			wantPath: "/models/gemini-2.5-flash:generateContent",
			headers:  map[string]string{"x-goog-api-key": "the-key"},
			reply:    `{"candidates":[{"content":{"parts":[{"text":"h"},{"text":"i"}]}}]}`,
			check: func(t *testing.T, payload map[string]any) {
				instr := payload["system_instruction"].(map[string]any)
				parts := instr["parts"].([]any)
				if parts[0].(map[string]any)["text"] != "SYS" {
					t.Errorf("system_instruction = %v", instr)
				}
				contents := payload["contents"].([]any)
				if len(contents) != 1 {
					t.Fatalf("contents = %v", contents)
				}
				turn := contents[0].(map[string]any)
				if turn["role"] != "user" {
					t.Errorf("contents[0].role = %v", turn["role"])
				}
				gc := payload["generationConfig"].(map[string]any)
				if gc["maxOutputTokens"] != float64(1024) {
					t.Errorf("maxOutputTokens = %v", gc["maxOutputTokens"])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var payload map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				for k, want := range tc.headers {
					if got := r.Header.Get(k); got != want {
						t.Errorf("header %s = %q, want %q", k, got, want)
					}
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q", got)
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(tc.reply))
			}))
			defer srv.Close()

			p, err := provider.Build(configFor(t, tc.name, srv.URL),
				envOf(map[string]string{tc.envKey: "the-key"}), srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			if p.Name() != tc.name {
				t.Errorf("Name() = %q, want %q", p.Name(), tc.name)
			}

			s, err := p.Start(context.Background(), "SYS")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Close() }()

			got, err := s.Send(context.Background(), "ask")
			if err != nil {
				t.Fatal(err)
			}
			if got != "hi" {
				t.Errorf("Send = %q, want the reply extracted from the dialect's envelope", got)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
			tc.check(t, payload)
		})
	}
}

// Every declared provider must build from its own key variable alone: the
// generic AUTOGIT_API_KEY would mask a spec whose EnvKey is a typo.
func TestBuildCoversEveryDeclaredProvider(t *testing.T) {
	for _, spec := range config.ProviderSpecs() {
		env := envOf(nil)
		if spec.EnvKey != "" {
			env = envOf(map[string]string{spec.EnvKey: "k"})
		}
		cfg := config.Default()
		cfg.Provider = spec.Name
		p, err := provider.Build(&cfg, env, nil)
		if err != nil {
			t.Errorf("Build(%s) = %v", spec.Name, err)
			continue
		}
		if p.Name() != spec.Name {
			t.Errorf("Build(%s) built %q", spec.Name, p.Name())
		}
	}
}

func TestBuildRejectsAnUnknownProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = "llama-cpp"
	_, err := provider.Build(&cfg, envOf(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "llama-cpp") {
		t.Fatalf("err = %v, want the unknown name reported", err)
	}
	for _, name := range provider.Names() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not list %q: %v", name, err)
		}
	}
}

func TestAPIKeyIsRequiredUnlessTheEndpointIsLocal(t *testing.T) {
	for _, spec := range config.ProviderSpecs() {
		if spec.HTTP == nil {
			continue
		}
		cfg := config.Default()
		cfg.Provider = spec.Name
		if _, err := provider.Build(&cfg, envOf(nil), nil); err == nil {
			t.Errorf("Build(%s) succeeded against the vendor endpoint with no key", spec.Name)
		}
	}

	// A compatible server behind a different baseUrl needs no key.
	cfg := configFor(t, "openai", "http://127.0.0.1:11434/v1")
	if _, err := provider.Build(cfg, envOf(nil), nil); err != nil {
		t.Errorf("Build(openai) against a local server: %v", err)
	}
}
