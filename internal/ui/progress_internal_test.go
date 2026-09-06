package ui

import (
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// buffer is what the spinner writes into. The lock is for the test, not for
// the production code: the drawing goroutine writes while the test reads.
type buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// clock hands out one prepared instant per call, so a test never waits on real
// time and never depends on how the goroutine is scheduled. The first call is
// the spinner's start; each tick then takes the next.
type clock struct {
	mu     sync.Mutex
	base   time.Time
	offset []time.Duration
	i      int
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.i >= len(c.offset) {
		return c.base.Add(c.offset[len(c.offset)-1])
	}
	at := c.base.Add(c.offset[c.i])
	c.i++
	return at
}

type harness struct {
	s     *spinner
	out   *buffer
	ticks chan time.Time
}

// tick delivers one tick and returns once the spinner has taken it. The
// spinner is single-threaded, so a send that completes also proves the
// previous tick was fully handled.
func (h *harness) tick() { h.ticks <- time.Time{} }

func start(label string, esc bool, at ...time.Duration) *harness {
	out := &buffer{}
	ticks := make(chan time.Time)
	c := &clock{base: time.Unix(0, 0), offset: at}
	s := &spinner{
		w:      out,
		label:  label,
		frames: frames(!esc),
		esc:    esc,
		now:    c.now,
		ticks:  ticks,
		done:   make(chan struct{}),
	}
	s.start()
	return &harness{s: s, out: out, ticks: ticks}
}

func TestNothingIsDrawnBeforeTheThreshold(t *testing.T) {
	h := start("Generating commit message…", true, 0, 100*time.Millisecond, 200*time.Millisecond, 250*time.Millisecond)
	h.tick()
	h.tick()
	// The third send proves the second tick was handled and drew nothing.
	h.tick()

	if got := h.out.String(); got != "" {
		t.Errorf("the spinner drew %q before the threshold", got)
	}
	h.s.stop()
}

func TestStopAfterNothingWasDrawnWritesNothing(t *testing.T) {
	h := start("Generating commit message…", true, 0, 100*time.Millisecond)
	h.tick()
	h.s.stop()

	if got := h.out.String(); got != "" {
		t.Errorf("a fast answer left %q behind", got)
	}
}

func TestElapsedSecondsAdvanceWithTheClock(t *testing.T) {
	h := start("Generating commit message…", true, 0, time.Second, 2*time.Second)
	h.tick()
	h.tick()
	h.s.stop()

	got := h.out.String()
	for _, want := range []string{"Generating commit message… 1s", "Generating commit message… 2s"} {
		if !strings.Contains(got, want) {
			t.Errorf("the counter never said %q:\n%q", want, got)
		}
	}
}

func TestStopErasesTheLine(t *testing.T) {
	h := start("Generating branch name…", true, 0, time.Second)
	h.tick()
	h.s.stop()

	if got := h.out.String(); !strings.HasSuffix(got, "\r\x1b[K") {
		t.Errorf("the line was left on the screen:\n%q", got)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	h := start("Generating branch name…", true, 0, time.Second)
	h.tick()
	h.s.stop()
	first := h.out.String()
	h.s.stop()

	if got := h.out.String(); got != first {
		t.Errorf("the second stop wrote again:\n%q", got)
	}
}

func TestDumbTerminalUsesNoEscapeSequences(t *testing.T) {
	h := start("Generating commit message…", false, 0, time.Second)
	h.tick()
	h.s.stop()

	got := h.out.String()
	if strings.Contains(got, "\x1b") {
		t.Errorf("a dumb terminal was sent a control sequence:\n%q", got)
	}
	for _, frame := range brailleFrames {
		if strings.Contains(got, frame) {
			t.Errorf("a dumb terminal was sent braille %q:\n%q", frame, got)
		}
	}
	if !strings.Contains(got, "\r") {
		t.Errorf("nothing was redrawn in place:\n%q", got)
	}
}

func TestLongLabelIsTruncated(t *testing.T) {
	h := start(strings.Repeat("word ", 40), true, 0, time.Second)
	h.tick()
	h.s.stop()

	for _, line := range strings.Split(h.out.String(), "\r") {
		line = strings.TrimSuffix(line, "\x1b[K")
		if n := utf8.RuneCountInString(line); n > maxLineRunes {
			t.Errorf("a line ran to %d runes, want at most %d:\n%q", n, maxLineRunes, line)
		}
	}
}

func TestDumbTerminalIsTheDefaultlessOne(t *testing.T) {
	for _, term := range []string{"", "dumb"} {
		if !dumbTerminal(term) {
			t.Errorf("TERM=%q was taken for a capable terminal", term)
		}
	}
	if dumbTerminal("xterm-256color") {
		t.Error("xterm-256color was taken for a dumb terminal")
	}
}
