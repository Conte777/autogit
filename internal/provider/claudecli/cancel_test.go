package claudecli

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A turn cancelled while the process is still streaming leaves more events
// behind than the channel holds; the reader must not block forever on a
// channel nobody drains any more.
func TestCancellationLeavesNoBlockedReader(t *testing.T) {
	bin, _ := fakeClaude(t, `
read -r line
printf '%s\n' '{"type":"result","subtype":"success","result":"ok"}'
while :; do printf '%s\n' '{"type":"system","subtype":"tick"}'; done
`)

	s, err := (&Provider{Binary: bin}).Start(context.Background(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	sess, ok := s.(*session)
	if !ok {
		t.Fatalf("Start returned %T", s)
	}
	if _, err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.Send(ctx, "again"); err == nil {
		t.Fatal("Send returned nil after cancellation")
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
	if sess.cmd.ProcessState == nil {
		t.Error("the killed process was never reaped")
	}

	deadline := time.Now().Add(5 * time.Second)
	for readStdoutRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the stdout reader is still blocked after the session was killed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readStdoutRunning() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), "claudecli.(*session).readStdout")
}
