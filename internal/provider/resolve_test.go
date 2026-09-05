package provider

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/provider/httpchat"
)

func httpSpecs(t *testing.T) []config.ProviderSpec {
	t.Helper()
	var out []config.ProviderSpec
	for _, spec := range config.ProviderSpecs() {
		if spec.HTTP != nil {
			out = append(out, spec)
		}
	}
	if len(out) == 0 {
		t.Fatal("no HTTP providers declared")
	}
	return out
}

// The declared defaults are the ones a blank config section resolves to. This
// exercises the merge, not the assignment Default() makes — a test comparing
// Default() against the spec it is built from could never fail.
func TestABlankSectionResolvesToTheDeclaredDefaults(t *testing.T) {
	for _, spec := range httpSpecs(t) {
		t.Run(spec.Name, func(t *testing.T) {
			p := config.Default().Providers
			*spec.HTTP(&p) = config.HTTPProvider{}

			s, err := resolve(spec, p, "k")
			if err != nil {
				t.Fatal(err)
			}
			if s.Model != spec.HTTPDefaults.Model {
				t.Errorf("model = %q, want the declared default %q", s.Model, spec.HTTPDefaults.Model)
			}
			if s.BaseURL != spec.HTTPDefaults.BaseURL {
				t.Errorf("baseURL = %q, want the declared default %q", s.BaseURL, spec.HTTPDefaults.BaseURL)
			}
			if spec.MaxTokensRequired && s.MaxTokens != spec.HTTPDefaults.MaxTokens {
				t.Errorf("maxTokens = %d, want the declared default %d", s.MaxTokens, spec.HTTPDefaults.MaxTokens)
			}
		})
	}
}

// The defaults must survive all the way onto the wire, not just into Settings.
func TestDefaultsReachTheRequest(t *testing.T) {
	for _, spec := range httpSpecs(t) {
		t.Run(spec.Name, func(t *testing.T) {
			p := config.Default().Providers
			*spec.HTTP(&p) = config.HTTPProvider{}
			s, err := resolve(spec, p, "k")
			if err != nil {
				t.Fatal(err)
			}

			req, err := dialects[spec.Name](s).Request(context.Background(), "sys",
				[]httpchat.Message{{Role: "user", Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(req.URL.String(), spec.HTTPDefaults.BaseURL) {
				t.Errorf("URL = %s, want it under the declared default %s", req.URL, spec.HTTPDefaults.BaseURL)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), spec.HTTPDefaults.Model) &&
				!strings.Contains(req.URL.Path, spec.HTTPDefaults.Model) {
				t.Errorf("neither the URL nor the payload names the default model %q", spec.HTTPDefaults.Model)
			}
		})
	}
}

// A trailing slash in baseUrl is a user's spelling, not a different endpoint.
func TestATrailingSlashInBaseURLIsNotDoubled(t *testing.T) {
	for _, spec := range httpSpecs(t) {
		t.Run(spec.Name, func(t *testing.T) {
			p := config.Default().Providers
			spec.HTTP(&p).BaseURL = "https://example.test/v1/"

			s, err := resolve(spec, p, "k")
			if err != nil {
				t.Fatal(err)
			}
			req, err := dialects[spec.Name](s).Request(context.Background(), "sys", nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(req.URL.Path, "//") {
				t.Errorf("URL = %s, want no doubled slash", req.URL)
			}
		})
	}
}

// An explicit maxTokens of 0 means "let the server decide" for the dialects
// whose API allows the field to be absent. Only the Messages API requires it.
func TestExplicitZeroMaxTokensIsHonouredWhereTheAPIAllowsIt(t *testing.T) {
	for _, spec := range httpSpecs(t) {
		t.Run(spec.Name, func(t *testing.T) {
			p := config.Default().Providers
			spec.HTTP(&p).MaxTokens = 0

			s, err := resolve(spec, p, "k")
			if err != nil {
				t.Fatal(err)
			}
			if spec.MaxTokensRequired {
				if s.MaxTokens <= 0 {
					t.Fatalf("maxTokens = %d, but %s requires the field", s.MaxTokens, spec.Name)
				}
				return
			}
			if s.MaxTokens != 0 {
				t.Fatalf("maxTokens = %d, want the explicit 0 kept", s.MaxTokens)
			}

			req, err := dialects[spec.Name](s).Request(context.Background(), "sys", nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"max_tokens", "max_completion_tokens", "generationConfig"} {
				if _, found := payload[key]; found {
					t.Errorf("payload carries %q, want no cap sent at all: %s", key, body)
				}
			}
		})
	}
}
