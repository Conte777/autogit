package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Progress reports that a slow operation is running, so a terminal does not
// look hung. Start returns the function that takes the report back down: it is
// the erase as much as the halt, so call it on every path out, and it is safe
// to call more than once.
type Progress interface {
	Start(label string) (stop func())
}

const (
	tickInterval = 100 * time.Millisecond
	// Below this an answer arrives before a spinner would mean anything, and
	// drawing one only makes the line flash.
	drawThreshold = 300 * time.Millisecond
	// The width of a terminal is not knowable without x/term, and COLUMNS is
	// rarely exported. The longest real line is 32 columns, so this only ever
	// catches a label that has no business being a label.
	maxLineRunes = 60
)

var (
	brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	asciiFrames   = []string{"|", "/", "-", "\\"}
)

// Start animates when the terminal is live on both sides and stderr is one
// too; anywhere else it prints the single static line.
func (u *UI) Start(label string) func() {
	if !u.interactive || !u.errTTY {
		return staticLine(u.err, label)
	}
	return animate(u.err, label)
}

// StaticProgress is the report without the animation, which is what --no-input
// gets: one line, no frames, no counter.
func (u *UI) StaticProgress() Progress { return staticProgress{w: u.err} }

type staticProgress struct{ w io.Writer }

func (s staticProgress) Start(label string) func() { return staticLine(s.w, label) }

func staticLine(w io.Writer, label string) func() {
	_, _ = fmt.Fprintln(w, label)
	return func() {}
}

// spinner redraws one line in place. now and ticks are seams: the tests drive
// both by hand, so no test waits on a real clock.
type spinner struct {
	w      io.Writer
	label  string
	frames []string
	// esc is false on a dumb terminal, which is exactly the terminal that
	// prints ^[[K literally instead of erasing.
	esc   bool
	now   func() time.Time
	ticks <-chan time.Time

	done  chan struct{}
	wg    sync.WaitGroup
	once  sync.Once
	drawn bool
	width int
}

func animate(w io.Writer, label string) func() {
	ticker := time.NewTicker(tickInterval)
	dumb := dumbTerminal(os.Getenv("TERM"))
	s := &spinner{
		w:      w,
		label:  label,
		frames: frames(dumb),
		esc:    !dumb,
		now:    time.Now,
		ticks:  ticker.C,
		done:   make(chan struct{}),
	}
	s.start()
	return func() {
		s.stop()
		ticker.Stop()
	}
}

func dumbTerminal(term string) bool { return term == "" || term == "dumb" }

func frames(dumb bool) []string {
	if dumb {
		return asciiFrames
	}
	return brailleFrames
}

func (s *spinner) start() {
	s.wg.Add(1)
	go s.run()
}

func (s *spinner) run() {
	defer s.wg.Done()
	started := s.now()
	frame := 0
	for {
		select {
		case <-s.done:
			return
		case <-s.ticks:
			elapsed := s.now().Sub(started)
			if elapsed < drawThreshold {
				continue
			}
			s.draw(s.frames[frame%len(s.frames)], elapsed)
			frame++
		}
	}
}

// stop waits for the drawing goroutine before erasing, so the last frame
// cannot land on the screen after the line was wiped.
func (s *spinner) stop() {
	s.once.Do(func() {
		close(s.done)
		s.wg.Wait()
		s.erase()
	})
}

func (s *spinner) draw(frame string, elapsed time.Duration) {
	line := truncate(fmt.Sprintf("%s %s %ds", frame, s.label, int(elapsed.Seconds())), maxLineRunes)
	s.drawn = true
	if s.esc {
		_, _ = fmt.Fprintf(s.w, "\r%s\x1b[K", line)
		return
	}
	n := utf8.RuneCountInString(line)
	if n > s.width {
		s.width = n
	}
	_, _ = fmt.Fprintf(s.w, "\r%s%s", line, strings.Repeat(" ", s.width-n))
}

// erase stays quiet when nothing was drawn: a fast answer must not leave even
// a control sequence behind.
func (s *spinner) erase() {
	if !s.drawn {
		return
	}
	if s.esc {
		_, _ = fmt.Fprint(s.w, "\r\x1b[K")
		return
	}
	_, _ = fmt.Fprintf(s.w, "\r%s\r", strings.Repeat(" ", s.width))
}

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
