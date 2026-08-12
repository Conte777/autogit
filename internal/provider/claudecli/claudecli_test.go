package claudecli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes a shell script that stands in for the real binary and
// records the argv it was called with.
func fakeClaude(t *testing.T, body string) (bin, argvLog string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "claude")
	argvLog = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvLog + "\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argvLog
}

const echoTwoTurns = `
while IFS= read -r line; do
  n=$((${n:-0} + 1))
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"turn '"$n"'"}]}}'
  printf '%s\n' '{"type":"result","subtype":"success"}'
done
`

func TestSessionTwoTurns(t *testing.T) {
	bin, argvLog := fakeClaude(t, echoTwoTurns)
	p := &Provider{Binary: bin, Model: "haiku"}

	s, err := p.Start(context.Background(), "SYSTEM PROMPT")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	first, err := s.Send(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if first != "turn 1" {
		t.Errorf("first reply = %q, want %q", first, "turn 1")
	}

	second, err := s.Send(context.Background(), "correct it")
	if err != nil {
		t.Fatal(err)
	}
	if second != "turn 2" {
		t.Errorf("second reply = %q; a correction must reuse the live process", second)
	}

	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--setting-sources=", "--tools", "--system-prompt", "SYSTEM PROMPT", "--model", "haiku"} {
		if !strings.Contains(string(argv), want) {
			t.Errorf("argv missing %q:\n%s", want, argv)
		}
	}
}

func TestSessionSkipsGarbageLines(t *testing.T) {
	bin, _ := fakeClaude(t, `
read -r line
echo 'this is not json at all'
echo '{"type":"system","subtype":"init"}'
echo '{ broken json'
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"feat: add thing"}]}}'
printf '%s\n' '{"type":"result","subtype":"success"}'
`)

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	got, err := s.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("garbage on stdout broke the parser: %v", err)
	}
	if got != "feat: add thing" {
		t.Errorf("reply = %q, want only the text blocks", got)
	}
}

func TestSessionReportsStderrWhenProcessDies(t *testing.T) {
	bin, _ := fakeClaude(t, `
read -r line
echo 'Not logged in. Run /login.' >&2
exit 1
`)

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	_, err = s.Send(context.Background(), "go")
	if err == nil {
		t.Fatal("Send succeeded although the process died")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("error drops stderr, which is the only diagnostic there is: %v", err)
	}
}

func TestSessionReportsErrorResult(t *testing.T) {
	bin, _ := fakeClaude(t, `
read -r line
printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"credit balance too low"}'
`)

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Send(context.Background(), "go"); err == nil ||
		!strings.Contains(err.Error(), "credit balance") {
		t.Fatalf("err = %v, want the result payload surfaced", err)
	}
}

func TestSessionCancellationKillsProcess(t *testing.T) {
	bin, _ := fakeClaude(t, "read -r line\nsleep 60\n")

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, sendErr := s.Send(ctx, "go")
		done <- sendErr
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send returned nil after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send ignored ctx cancellation and blocked on the pipe")
	}
}

func TestStartFailsOnMissingBinary(t *testing.T) {
	_, err := (&Provider{Binary: filepath.Join(t.TempDir(), "nope")}).Start(context.Background(), "sys")
	if err == nil {
		t.Fatal("Start succeeded with a missing binary")
	}
}

func TestCloseIsIdempotentAndKillsHungProcess(t *testing.T) {
	bin, _ := fakeClaude(t, "sleep 60\n")

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
	if elapsed := time.Since(start); elapsed > closeGrace+3*time.Second {
		t.Errorf("Close took %s; it must not wait for a hung process forever", elapsed)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil", err)
	}
}

func TestName(t *testing.T) {
	if got := (&Provider{}).Name(); got != "claude-cli" {
		t.Errorf("Name() = %q", got)
	}
}
