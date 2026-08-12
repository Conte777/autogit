// Package claudecli drives the user's own `claude` binary over stream-json.
// This is the only legitimate route to a Claude subscription — Anthropic does
// not permit third-party tools to hold Free/Pro/Max credentials — so autogit
// talks to the already-installed, already-logged-in client.
package claudecli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Conte777/autogit/internal/gen"
)

const (
	defaultBinary = "claude"
	closeGrace    = 3 * time.Second
	// stdout is NDJSON; a single line can carry a whole assistant turn.
	maxLine = 8 << 20
)

// Provider starts one `claude` process per session.
type Provider struct {
	Binary string
	Model  string
	// ExtraArgs is appended verbatim, for flags autogit does not model.
	ExtraArgs []string
}

func (p *Provider) Name() string { return "claude-cli" }

func (p *Provider) binary() string {
	if p.Binary != "" {
		return p.Binary
	}
	return defaultBinary
}

func (p *Provider) args(system string) []string {
	args := []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		// Empty value, not omitted: this is what isolates the child from
		// CLAUDE.md, skills, commands, hooks and MCP servers.
		"--setting-sources=",
		// The SDK omits this flag when the tool list is empty, which silently
		// leaves Bash and Read available to a message generator.
		"--tools", "",
		"--system-prompt", system,
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return append(args, p.ExtraArgs...)
}

// Start launches the process and begins draining its pipes.
func (p *Provider) Start(ctx context.Context, system string) (gen.Session, error) {
	cmd := exec.Command(p.binary(), p.args(system)...) //nolint:noctx // ctx drives Send/Close, not the process lifetime
	cmd.Env = childEnv()
	setProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start %s: %w", p.binary(), err)
	}

	s := &session{
		cmd:    cmd,
		stdin:  stdin,
		events: make(chan event, 64),
	}
	go s.readStdout(stdout)
	go s.readStderr(stderr)
	return s, nil
}

// childEnv guards against the child re-entering autogit through the user's own
// UserPromptSubmit hook.
func childEnv() []string {
	env := os.Environ()
	env = append(env, "AUTOGIT_ACTIVE=1", "CLAUDE_CODE_ENTRYPOINT=autogit")
	return env
}

type event struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Result string `json:"result"`
}

type session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan event

	stderrMu sync.Mutex
	stderr   []string

	closeOnce sync.Once
	closeErr  error
}

// Send posts one turn and reads events until the process reports a result.
func (s *session) Send(ctx context.Context, text string) (string, error) {
	line, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	})
	if err != nil {
		return "", err
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		return "", fmt.Errorf("claude stdin closed: %w%s", err, s.stderrTail())
	}

	var reply strings.Builder
	for {
		select {
		case <-ctx.Done():
			s.kill()
			return "", ctx.Err()
		case ev, ok := <-s.events:
			if !ok {
				return "", fmt.Errorf("claude exited before answering%s", s.stderrTail())
			}
			switch ev.Type {
			case "assistant":
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						reply.WriteString(block.Text)
					}
				}
			case "result":
				if ev.IsError || (ev.Subtype != "" && ev.Subtype != "success") {
					return reply.String(), fmt.Errorf("claude returned %s: %s%s",
						ev.Subtype, ev.Result, s.stderrTail())
				}
				if reply.Len() == 0 {
					// Some builds carry the whole answer only in the result event.
					reply.WriteString(ev.Result)
				}
				return reply.String(), nil
			}
		}
	}
}

func (s *session) readStdout(r io.Reader) {
	defer close(s.events)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		var ev event
		// A non-JSON line is a banner or a warning, not a protocol error.
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		s.events <- ev
	}
	if err := sc.Err(); err != nil {
		s.note("autogit: cannot read claude stdout: " + err.Error())
	}
}

func (s *session) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 8*1024), 1<<20)
	for sc.Scan() {
		s.note(sc.Text())
	}
}

func (s *session) note(line string) {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	s.stderr = append(s.stderr, line)
	if len(s.stderr) > 20 {
		s.stderr = s.stderr[len(s.stderr)-20:]
	}
}

// stderrTail is the only diagnostic there is when the process dies.
func (s *session) stderrTail() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	if len(s.stderr) == 0 {
		return ""
	}
	return "\nclaude stderr:\n  " + strings.Join(s.stderr, "\n  ")
}

// Close shuts stdin, waits briefly, then kills the whole process group.
func (s *session) Close() error {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()

		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()

		select {
		case err := <-done:
			var xe *exec.ExitError
			if err != nil && !errors.As(err, &xe) {
				s.closeErr = err
			}
		case <-time.After(closeGrace):
			killGroup(s.cmd)
			<-done
		}
	})
	return s.closeErr
}

func (s *session) kill() {
	killGroup(s.cmd)
}
