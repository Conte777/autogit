package claudecli

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A process that streams past the end of a turn fills the event channel while
// nobody is in Send; cancelling the next turn must not strand the reader on a
// channel nobody will drain again.
func TestCancellationLeavesNoBlockedReader(t *testing.T) {
	bin, _ := fakeClaude(t, `
read -r line
printf '%s\n' '{"type":"result","subtype":"success","result":"ok"}'
i=0
while [ $i -lt 500 ]; do
  printf '%s\n' '{"type":"system","subtype":"tick"}'
  i=$((i + 1))
done
sleep 60
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
	waitForFullBuffer(t, sess)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
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
	for stdoutReaderRunning() {
		if time.Now().After(deadline) {
			t.Fatal("the stdout reader is still blocked after the session was killed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The reader is only stuck once the buffer is full and it has one more event
// in hand, so the test waits for that state instead of racing it.
func waitForFullBuffer(t *testing.T, s *session) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(s.events) < cap(s.events) {
		if time.Now().After(deadline) {
			t.Fatalf("the process never filled the event channel: %d/%d", len(s.events), cap(s.events))
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}

func stdoutReaderRunning() bool {
	buf := make([]byte, 64*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Contains(string(buf[:n]), "claudecli.(*session).readStdout")
		}
		buf = make([]byte, 2*len(buf))
	}
}
