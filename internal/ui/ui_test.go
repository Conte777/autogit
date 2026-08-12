package ui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/ui"
)

func newUI(input string) (*ui.UI, *strings.Builder, *strings.Builder) {
	var out, errw strings.Builder
	return ui.New(&out, &errw, strings.NewReader(input), true), &out, &errw
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		input string
		def   bool
		want  bool
	}{
		{input: "y\n", want: true},
		{input: "YES\n", want: true},
		{input: "n\n", def: true},
		{input: "\n", def: true, want: true},
		{input: "\n", def: false, want: false},
		{input: "what?\nyes\n", want: true},
	}

	for _, tt := range tests {
		u, _, _ := newUI(tt.input)
		got, err := u.Confirm("go?", tt.def)
		if err != nil {
			t.Fatalf("Confirm(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("Confirm(%q, def=%v) = %v, want %v", tt.input, tt.def, got, tt.want)
		}
	}
}

func TestChoose(t *testing.T) {
	u, _, errw := newUI("t\n")
	got, err := u.Choose("what?", []ui.Option{
		{Key: "a", Label: "everything"},
		{Key: "t", Label: "tracked only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "t" {
		t.Errorf("Choose() = %q", got)
	}
	if !strings.Contains(errw.String(), "tracked only") {
		t.Errorf("the options were never shown:\n%s", errw.String())
	}
}

func TestQuestionsGoToStderrNotStdout(t *testing.T) {
	u, out, errw := newUI("y\n")
	if _, err := u.Confirm("go?", false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("a question leaked onto stdout: %q", out.String())
	}
	if errw.String() == "" {
		t.Error("the question was never printed")
	}
}

func TestNonInteractiveRefusesToAsk(t *testing.T) {
	var out, errw strings.Builder
	u := ui.New(&out, &errw, strings.NewReader("y\n"), false)

	if _, err := u.Confirm("go?", false); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Confirm() = %v, want ErrNoInput", err)
	}
	if _, err := u.Choose("what?", []ui.Option{{Key: "a"}}); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Choose() = %v, want ErrNoInput", err)
	}
}

func TestClosedInputIsNotAnInfiniteLoop(t *testing.T) {
	u, _, _ := newUI("")
	if _, err := u.Confirm("go?", false); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Confirm() on EOF = %v, want ErrNoInput", err)
	}
	u, _, _ = newUI("garbage\n")
	if _, err := u.Choose("what?", []ui.Option{{Key: "a"}}); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Choose() on unmatched input then EOF = %v, want ErrNoInput", err)
	}
}

func TestNoopAnswersNothing(t *testing.T) {
	var n ui.Noop
	if n.Interactive() {
		t.Error("Noop claims to be interactive")
	}
	if _, err := n.Confirm("go?", true); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Confirm() = %v", err)
	}
	if _, err := n.Choose("what?", nil); !errors.Is(err, ui.ErrNoInput) {
		t.Errorf("Choose() = %v", err)
	}
}

func TestPrintGoesToStdout(t *testing.T) {
	u, out, errw := newUI("")
	u.Print("committed %s", "abc1234")
	u.Warn("a warning")

	if out.String() != "committed abc1234\n" {
		t.Errorf("stdout = %q", out.String())
	}
	if errw.String() != "a warning\n" {
		t.Errorf("stderr = %q", errw.String())
	}
}
