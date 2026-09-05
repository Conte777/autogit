package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
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

// The exit codes are a contract, and cobra's own answer to a misspelled command
// is a plain error, which would report a bug in autogit instead of a typo.
func TestUnknownCommandIsAUsageError(t *testing.T) {
	for _, name := range []string{"install", "uninstall", "commt"} {
		t.Run(name, func(t *testing.T) {
			err := runRoot(t, name)
			if err == nil {
				t.Fatalf("autogit %s was accepted", name)
			}
			if got := ExitCode(err); got != ExitUsage {
				t.Errorf("ExitCode() = %d, want %d (%v)", got, ExitUsage, err)
			}
		})
	}

	t.Run("suggests the command that was meant", func(t *testing.T) {
		err := runRoot(t, "commt")
		if err == nil || !strings.Contains(err.Error(), "Did you mean this?\n\tcommit") {
			t.Errorf("no suggestion for a near miss: %v", err)
		}
	})

	// RunE only exists so that cobra reaches Args at all: it stops at the first
	// command that is not runnable. Take it away and the bare invocation is the
	// only thing that notices.
	t.Run("the bare command still helps", func(t *testing.T) {
		if err := runRoot(t); err != nil {
			t.Errorf("autogit with no arguments failed: %v", err)
		}
	})
}

func runRoot(t *testing.T, args ...string) error {
	t.Helper()
	root := Root(Version{})
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.ExecuteContext(context.Background())
}
