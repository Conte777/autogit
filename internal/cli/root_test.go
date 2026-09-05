package cli

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/Conte777/autogit/internal/ui"
)

// The bug --no-input carried: autogit went quiet and git did not. openRepo has
// to read git's permission to ask from the prompter, so the two cannot part.
func TestNoInputReachesGit(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	terminal := ui.New(os.Stdout, os.Stderr, os.Stdin, true)

	silent, err := openRepo(context.Background(), dir, prompterFor(&globals{noInput: true}, terminal))
	if err != nil {
		t.Fatal(err)
	}
	if silent.Interactive() {
		t.Error("--no-input left git free to stop and ask on the terminal")
	}

	asking, err := openRepo(context.Background(), dir, prompterFor(&globals{}, terminal))
	if err != nil {
		t.Fatal(err)
	}
	if !asking.Interactive() {
		t.Error("a terminal run took git's own prompts away")
	}
}
