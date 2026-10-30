package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/proc"
)

type Provider struct {
	Binary string
	Model  string
}

func (p *Provider) Name() string { return "codex-cli" }

func (p *Provider) Start(_ context.Context, system string) (gen.Session, error) {
	binary := p.Binary
	if binary == "" {
		binary = "codex"
	}
	if !filepath.IsAbs(binary) && strings.ContainsAny(binary, `/\`) {
		absolute, err := filepath.Abs(binary)
		if err != nil {
			return nil, fmt.Errorf("resolve codex binary: %w", err)
		}
		binary = absolute
	}
	return &session{binary: binary, model: p.Model, history: []message{{Role: "system", Content: system}}}, nil
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type session struct {
	binary  string
	model   string
	history []message
}

func (s *session) Send(ctx context.Context, text string) (string, error) {
	dir, err := os.MkdirTemp("", "autogit-codex-")
	if err != nil {
		return "", fmt.Errorf("create codex working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	output := filepath.Join(dir, "answer")
	args := []string{
		"exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check",
		"--sandbox", "read-only",
		"--disable", "shell_tool", "--disable", "apps", "--disable", "hooks",
		"--disable", "multi_agent", "--disable", "skill_search",
		"-c", `forced_login_method="chatgpt"`, "-c", "project_doc_max_bytes=0",
		"-C", dir, "--output-last-message", output,
	}
	if s.model != "" {
		args = append(args, "--model", s.model)
	}
	args = append(args, "-")
	s.history = append(s.history, message{Role: "user", Content: text})
	history, err := json.Marshal(s.history)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, s.binary, args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "OPENAI_API_KEY" && key != "CODEX_API_KEY" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Stdin = strings.NewReader("Continue this conversation. Follow the system message and answer the last user message. Return only the requested output.\n\n" + string(history))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	proc.Isolate(cmd)
	cmd.Cancel = func() error { return proc.Kill(cmd) }
	cmd.WaitDelay = 3 * time.Second
	if startErr := cmd.Start(); startErr != nil {
		return "", fmt.Errorf("cannot start %s: %w", s.binary, startErr)
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("codex exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	answer, err := os.ReadFile(filepath.Clean(output))
	if err != nil {
		return "", fmt.Errorf("read codex answer: %w", err)
	}
	reply := strings.TrimSpace(string(answer))
	if reply == "" {
		return "", fmt.Errorf("codex returned an empty answer")
	}
	s.history = append(s.history, message{Role: "assistant", Content: reply})
	return reply, nil
}

func (s *session) Close() error { return nil }
