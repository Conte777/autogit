package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/provider"
	"github.com/Conte777/autogit/internal/validate"
)

func codexProvider(t *testing.T, script string) (gen.Provider, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nset -eu\n"+`
log=$(dirname "$0")
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) output=$2; shift ;;
    --model) printf '%s' "$2" > "$log/model"; shift ;;
  esac
  shift
done
pwd > "$log/cwd"
cat > "$log/prompt"
`+script), 0o700); err != nil {
		t.Fatal(err)
	}
	doc, err := json.Marshal(map[string]any{
		"provider":  "codex-cli",
		"providers": map[string]any{"codex-cli": map[string]string{"binary": bin, "model": "test-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if writeErr := os.WriteFile(path, doc, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if schemaErr := config.ValidateDocument(doc); schemaErr != nil {
		t.Fatal(schemaErr)
	}
	cfg, err := config.Load(config.Options{GlobalPath: path, Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := provider.Build(cfg, envOf(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, dir
}

func readCodexFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func assertCodexCleaned(t *testing.T, dir string) {
	t.Helper()
	cwd := readCodexFile(t, filepath.Join(dir, "cwd"))
	if _, err := os.Stat(cwd); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary working directory remains: %s (%v)", cwd, err)
	}
}

func TestCodexRegeneratesWithHistoryAndIsolatesOutput(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("CODEX_API_KEY", "test-codex-key")
	p, dir := codexProvider(t, `
test -z "${OPENAI_API_KEY+x}"
test -z "${CODEX_API_KEY+x}"
printf 'unrelated stdout banner\n'
printf 'progress on stderr\n' >&2
if [ -e "$log/first" ]; then
  printf 'add-codex-provider\n' > "$output"
else
  cp "$log/prompt" "$log/first"
  printf 'INVALID/SLUG\n' > "$output"
fi
`)
	capture, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	original := os.Stdout
	os.Stdout = capture
	defer func() { os.Stdout = original }()
	result, err := gen.Generate(context.Background(), p, gen.Request{
		System: "Return a branch slug.", Prompt: "Add native Codex subscription support.",
		Validator: validate.SlugRules{}, Attempts: 2,
		Correction: func(string, []string) string { return "Use lowercase words separated by hyphens." },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != "add-codex-provider" || result.Attempts != 2 {
		t.Fatalf("Generate = %+v", result)
	}
	if _, seekErr := capture.Seek(0, io.SeekStart); seekErr != nil {
		t.Fatal(seekErr)
	}
	out, err := io.ReadAll(capture)
	if err != nil || len(out) != 0 {
		t.Errorf("provider leaked stdout: %q (%v)", out, err)
	}
	prompt := readCodexFile(t, filepath.Join(dir, "prompt"))
	for _, want := range []string{"Return a branch slug.", "Add native Codex subscription support.", "INVALID/SLUG", "Use lowercase words separated by hyphens."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("regeneration lost %q: %s", want, prompt)
		}
	}
	if got := readCodexFile(t, filepath.Join(dir, "model")); got != "test-model" {
		t.Errorf("model = %q", got)
	}
	assertCodexCleaned(t, dir)
}

func TestCodexRelativeBinary(t *testing.T) {
	_, dir := codexProvider(t, `printf 'relative-binary-works\n' > "$output"`)
	t.Chdir(dir)
	cfg, err := config.Load(config.Options{GlobalPath: filepath.Join(dir, "config.json"), Env: envOf(nil)})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers.CodexCLI.Binary = "./codex"
	p, err := provider.Build(cfg, envOf(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := gen.Generate(context.Background(), p, gen.Request{Prompt: "ask", Validator: validate.SlugRules{}})
	if err != nil || result.Value != "relative-binary-works" {
		t.Fatalf("Generate = %+v, %v", result, err)
	}
	assertCodexCleaned(t, dir)
}

func TestCodexSubprocessFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"exit", `printf 'login required\n' >&2; exit 17`, "login required"},
		{"missing answer", `exit 0`, "answer"},
		{"empty answer", `printf ' \n' > "$output"`, "empty"},
		{"missing binary", `exit 0`, "cannot start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, dir := codexProvider(t, tc.script)
			if tc.name == "missing binary" {
				if err := os.Remove(filepath.Join(dir, "codex")); err != nil {
					t.Fatal(err)
				}
			}
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			_, err := gen.Generate(context.Background(), p, gen.Request{Prompt: "ask", Validator: validate.SlugRules{}, Attempts: 2})
			var providerErr *gen.ProviderError
			if !errors.As(err, &providerErr) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Generate error = %v, want provider failure containing %q", err, tc.want)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 0 {
				t.Errorf("temporary files remain: %v (%v)", entries, err)
			}
		})
	}
}

func TestCodexCancellationCleansUp(t *testing.T) {
	p, dir := codexProvider(t, `
sleep 60 &
child=$!
printf '%s' "$child" > "$log/child"
wait "$child"
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if pid, err := os.ReadFile(filepath.Join(dir, "child")); err == nil && len(pid) > 0 {
					cancel()
					return
				}
			}
		}
	}()
	start := time.Now()
	_, err := gen.Generate(ctx, p, gen.Request{Prompt: "ask", Validator: validate.SlugRules{}})
	<-ready
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %s", elapsed)
	}
	pid := readCodexFile(t, filepath.Join(dir, "child"))
	state, psErr := exec.Command("ps", "-o", "stat=", "-p", pid).Output()
	var exitErr *exec.ExitError
	if psErr != nil && (!errors.As(psErr, &exitErr) || exitErr.ExitCode() != 1) {
		t.Fatalf("check child process: %v", psErr)
	}
	if psErr == nil && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		_ = exec.Command("kill", "-KILL", pid).Run()
		t.Errorf("child process %s remains after cancellation: %s", pid, state)
	}
	assertCodexCleaned(t, dir)
}
