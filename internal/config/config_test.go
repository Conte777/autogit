package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conte777/autogit/internal/config"
)

// envOf builds a lookup over a fixed map, so no test touches the real
// environment or the real ~/.config.
func envOf(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsWhenNoFiles(t *testing.T) {
	cfg, err := config.Load(config.Options{Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "claude-cli" || cfg.Preset != "conventional" || cfg.Attempts != 3 {
		t.Errorf("defaults = %+v", cfg)
	}
	if cfg.Timeout.Duration() != 90*time.Second {
		t.Errorf("Timeout = %s, want 90s", cfg.Timeout.Duration())
	}
}

func TestGlobalFileDeepMerges(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.json", `{
	  "provider": "anthropic",
	  "diff": { "maxBytes": 1000 }
	}`)

	cfg, err := config.Load(config.Options{GlobalPath: path, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("Provider = %q", cfg.Provider)
	}
	if cfg.Diff.MaxBytes != 1000 {
		t.Errorf("Diff.MaxBytes = %d", cfg.Diff.MaxBytes)
	}
	if cfg.Diff.Context != 3 {
		t.Errorf("Diff.Context = %d; an untouched sibling key must keep its default", cfg.Diff.Context)
	}
	if len(cfg.Diff.ExcludePathspecs) == 0 {
		t.Error("Diff.ExcludePathspecs was wiped by a partial diff object")
	}
	if cfg.Providers.ClaudeCLI.Binary != "claude" {
		t.Error("provider defaults were wiped")
	}
}

func TestRepoFileOverridesGlobal(t *testing.T) {
	globalDir, repoDir := t.TempDir(), t.TempDir()
	global := writeFile(t, globalDir, "config.json", `{"preset":"conventional","confirm":true}`)
	writeFile(t, repoDir, ".autogit.json", `{"preset":"ticket","protectedBranches":["trunk"]}`)

	cfg, err := config.Load(config.Options{GlobalPath: global, RepoRoot: repoDir, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preset != "ticket" {
		t.Errorf("Preset = %q, want the repo value", cfg.Preset)
	}
	if !cfg.Confirm {
		t.Error("Confirm was reset by a repo file that never mentioned it")
	}
	if len(cfg.ProtectedBranches) != 1 || cfg.ProtectedBranches[0] != "trunk" {
		t.Errorf("ProtectedBranches = %v", cfg.ProtectedBranches)
	}
}

func TestEnvBeatsFiles(t *testing.T) {
	dir := t.TempDir()
	global := writeFile(t, dir, "config.json", `{"provider":"anthropic","attempts":5}`)

	cfg, err := config.Load(config.Options{
		GlobalPath: global,
		Env: envOf(map[string]string{
			"AUTOGIT_PROVIDER": "openai",
			"AUTOGIT_ATTEMPTS": "2",
			"AUTOGIT_TIMEOUT":  "10s",
			"AUTOGIT_MODEL":    "gpt-5-mini",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" || cfg.Attempts != 2 || cfg.Timeout.Duration() != 10*time.Second {
		t.Errorf("env did not win: %+v", cfg)
	}
	if cfg.Model() != "gpt-5-mini" {
		t.Errorf("Model() = %q, want AUTOGIT_MODEL scoped to the selected provider", cfg.Model())
	}
	if cfg.Providers.Anthropic.Model != "claude-haiku-4-5" {
		t.Errorf("AUTOGIT_MODEL leaked into the anthropic provider: %q", cfg.Providers.Anthropic.Model)
	}
}

func TestUnknownKeyIsAnError(t *testing.T) {
	dir := t.TempDir()
	global := writeFile(t, dir, "config.json", `{"protectedBranch":["main"]}`)

	_, err := config.Load(config.Options{GlobalPath: global, Env: envOf(nil)})
	if err == nil {
		t.Fatal("a typo in protectedBranches silently disabled branch protection")
	}
	if !strings.Contains(err.Error(), "protectedBranch") {
		t.Errorf("err = %v, want it to name the unknown key", err)
	}
	var cerr *config.Error
	if !errors.As(err, &cerr) {
		t.Errorf("err = %T, want *config.Error so the CLI can exit 8", err)
	}
}

func TestAPIKeyInFileIsAnError(t *testing.T) {
	for _, body := range []string{
		`{"providers":{"anthropic":{"apiKey":"sk-leak"}}}`,
		`{"providers":{"openai":{"api_key":"sk-leak"}}}`,
		`{"token":"ghp_leak"}`,
	} {
		dir := t.TempDir()
		global := writeFile(t, dir, "config.json", body)
		_, err := config.Load(config.Options{GlobalPath: global, Env: envOf(nil)})
		if err == nil {
			t.Errorf("%s was accepted; a config file must never hold a credential", body)
			continue
		}
		if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
			t.Errorf("err = %v, want it to point at the environment", err)
		}
	}
}

func TestRepoFileCannotSetProvider(t *testing.T) {
	for _, body := range []string{
		`{"provider":"openai"}`,
		`{"providers":{"claude-cli":{"binary":"/tmp/evil"}}}`,
	} {
		repo := t.TempDir()
		writeFile(t, repo, ".autogit.json", body)

		_, err := config.Load(config.Options{RepoRoot: repo, Env: envOf(nil)})
		if err == nil {
			t.Fatalf("%s was accepted from an untrusted repository file", body)
		}
		if !strings.Contains(err.Error(), "global-only") {
			t.Errorf("err = %v, want it to explain that provider settings are global-only", err)
		}
	}
}

func TestRepoFileMayTuneDiffAndPresets(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, ".autogit.json", `{
	  "preset": "ticket",
	  "confirm": true,
	  "diff": { "maxBytes": 2048 },
	  "presets": { "ticket": { "commit": { "maxSubject": 60 } } }
	}`)

	cfg, err := config.Load(config.Options{RepoRoot: repo, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Diff.MaxBytes != 2048 || !cfg.Confirm {
		t.Errorf("cfg = %+v", cfg)
	}
	p, err := cfg.ResolvePreset()
	if err != nil {
		t.Fatal(err)
	}
	if p.Commit.MaxSubject != 60 {
		t.Errorf("MaxSubject = %d, want the repo override", p.Commit.MaxSubject)
	}
	if len(p.Commit.Types) == 0 {
		t.Error("a partial override wiped the built-in types")
	}
}

func TestPresetOverrideLayersStack(t *testing.T) {
	globalDir, repo := t.TempDir(), t.TempDir()
	global := writeFile(t, globalDir, "config.json",
		`{"preset":"ticket","presets":{"ticket":{"commit":{"maxSubject":60,"maxBodyLine":80}}}}`)
	writeFile(t, repo, ".autogit.json", `{"presets":{"ticket":{"commit":{"maxSubject":72}}}}`)

	cfg, err := config.Load(config.Options{GlobalPath: global, RepoRoot: repo, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.ResolvePreset()
	if err != nil {
		t.Fatal(err)
	}
	if p.Commit.MaxSubject != 72 {
		t.Errorf("MaxSubject = %d, want the repo layer on top", p.Commit.MaxSubject)
	}
	if p.Commit.MaxBodyLine != 80 {
		t.Errorf("MaxBodyLine = %d, want the global layer preserved", p.Commit.MaxBodyLine)
	}
}

func TestPromptPathResolvesAgainstDeclaringFile(t *testing.T) {
	globalDir, repo := t.TempDir(), t.TempDir()
	global := writeFile(t, globalDir, "config.json",
		`{"preset":"ticket","presets":{"ticket":{"commit":{"prompt":"prompts/commit.md"}}}}`)
	writeFile(t, repo, ".autogit.json",
		`{"presets":{"ticket":{"branch":{"prompt":".autogit/prompts/branch.md"}}}}`)

	cfg, err := config.Load(config.Options{GlobalPath: global, RepoRoot: repo, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.ResolvePreset()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(globalDir, "prompts/commit.md"); p.Commit.Prompt != want {
		t.Errorf("commit prompt = %q, want %q", p.Commit.Prompt, want)
	}
	if want := filepath.Join(repo, ".autogit/prompts/branch.md"); p.Branch.Prompt != want {
		t.Errorf("branch prompt = %q, want %q", p.Branch.Prompt, want)
	}
}

func TestUnknownPresetIsAConfigError(t *testing.T) {
	dir := t.TempDir()
	global := writeFile(t, dir, "config.json", `{"preset":"nope"}`)

	cfg, err := config.Load(config.Options{GlobalPath: global, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolvePreset(); err == nil {
		t.Fatal("ResolvePreset accepted an unknown preset name")
	}
}

func TestUnknownKeyInsidePresetOverride(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, ".autogit.json", `{"presets":{"conventional":{"commit":{"maxSubjekt":60}}}}`)

	cfg, err := config.Load(config.Options{RepoRoot: repo, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolvePreset(); err == nil {
		t.Fatal("a typo inside a preset override was accepted")
	}
}

// The preset name selects a file inside the embedded prompt assets, so an
// untrusted repository config must not be able to spell it.
func TestPresetNameIsNotAConfigKey(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, ".autogit.json", `{"presets":{"conventional":{"name":"../../etc"}}}`)

	cfg, err := config.Load(config.Options{RepoRoot: repo, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.ResolvePreset()
	if err == nil {
		t.Fatalf("a preset override named itself %q", p.Name())
	}
}

func TestResolvedPresetKnowsItsName(t *testing.T) {
	dir := t.TempDir()
	global := writeFile(t, dir, "config.json",
		`{"preset":"house","presets":{"house":{"commit":{"body":{"mode":"off"},"scope":{"mode":"off"}}}}}`)

	cfg, err := config.Load(config.Options{GlobalPath: global, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := cfg.ResolvePreset()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "house" {
		t.Errorf("Name() = %q, want the preset the layer defined", p.Name())
	}
}

func TestBadEnvValues(t *testing.T) {
	for _, kv := range []map[string]string{
		{"AUTOGIT_ATTEMPTS": "zero"},
		{"AUTOGIT_ATTEMPTS": "0"},
		{"AUTOGIT_TIMEOUT": "quickly"},
		{"AUTOGIT_CONFIRM": "maybe"},
	} {
		if _, err := config.Load(config.Options{Env: envOf(kv)}); err == nil {
			t.Errorf("Load accepted %v", kv)
		}
	}
}

func TestAPIKeyLookupOrder(t *testing.T) {
	env := envOf(map[string]string{"ANTHROPIC_API_KEY": "specific", "AUTOGIT_API_KEY": "generic"})
	if got := config.APIKey("anthropic", env); got != "specific" {
		t.Errorf("APIKey(anthropic) = %q", got)
	}
	if got := config.APIKey("gemini", env); got != "generic" {
		t.Errorf("APIKey(gemini) = %q, want the generic fallback", got)
	}
	if got := config.APIKey("claude-cli", envOf(nil)); got != "" {
		t.Errorf("APIKey(claude-cli) = %q, want empty", got)
	}
}

func TestTimeoutRoundTrip(t *testing.T) {
	var d config.Duration
	if err := json.Unmarshal([]byte(`"90s"`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Duration() != 90*time.Second {
		t.Errorf("Duration() = %s", d.Duration())
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"1m30s"` {
		t.Errorf("Marshal = %s", out)
	}
	if err := json.Unmarshal([]byte(`90`), &d); err == nil {
		t.Error("a bare number was accepted as a timeout")
	}
}

func TestSchemaValidatesTheDefaultConfig(t *testing.T) {
	raw, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "protectedBranches") {
		t.Errorf("schema is missing protectedBranches:\n%s", raw)
	}

	doc, err := json.Marshal(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateDocument(doc); err != nil {
		t.Errorf("the default config does not validate against its own schema: %v", err)
	}
	if err := config.ValidateDocument([]byte(`{"attempts":"three"}`)); err == nil {
		t.Error("ValidateDocument accepted a string where an integer belongs")
	}
}
