package cli

import (
	"os"
	"testing"

	"github.com/Conte777/autogit/internal/ui"
)

// --no-input has to reach git as well as autogit, and it does that by picking
// the prompter: openRepo reads git's permission to ask from the same value.
func TestNoInputPicksASilentPrompter(t *testing.T) {
	out := ui.New(os.Stdout, os.Stderr, os.Stdin, true)

	if p := prompterFor(&globals{noInput: true}, out); p.Interactive() {
		t.Error("--no-input still handed back a prompter that asks")
	}
	if p := prompterFor(&globals{}, out); !p.Interactive() {
		t.Error("a terminal run lost its prompter")
	}
}
