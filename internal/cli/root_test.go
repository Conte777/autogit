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

// The exit codes are a contract, and cobra's own answer to a bad invocation is
// a plain error — exit 1, which reports a bug in autogit instead of a typo —
// or, under a command it cannot run, no error at all.
func TestBadInvocationIsAUsageError(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a command that no longer exists", []string{"install"}},
		{"a misspelled command", []string{"commt"}},
		{"a bad flag on the root", []string{"--bogus"}},
		{"a bad flag on a subcommand", []string{"schema", "--bogus"}},
		{"a bad flag on a leaf command", []string{"preset", "list", "--bogus"}},
		{"a missing argument", []string{"preset", "eject"}},
		{"a surplus argument", []string{"schema", "extra"}},
		{"an unknown word after a parent command", []string{"preset", "bogus"}},
		// cobra builds these two on its way into Execute, after the tree is
		// handed over.
		{"an unknown word after cobra's own parent command", []string{"completion", "bogus"}},
		{"a surplus argument under cobra's own command", []string{"completion", "bash", "extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runRoot(t, tc.args...)
			if err == nil {
				t.Fatalf("autogit %s was accepted", strings.Join(tc.args, " "))
			}
			if got := ExitCode(err); got != ExitUsage {
				t.Errorf("ExitCode() = %d, want %d (%v)", got, ExitUsage, err)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		args  []string
		meant string
	}{
		{"the command that was meant", []string{"commt"}, "commit"},
		{"the subcommand that was meant", []string{"preset", "lst"}, "list"},
	} {
		t.Run("suggests "+tc.name, func(t *testing.T) {
			err := runRoot(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), "Did you mean this?\n\t"+tc.meant) {
				t.Errorf("no suggestion for a near miss: %v", err)
			}
		})
	}

	// RunE only exists so that cobra reaches Args at all: it stops at the first
	// command that is not runnable. Take it away and these two are the only
	// things that notice.
	for _, args := range [][]string{{}, {"preset"}} {
		t.Run("a parent command still helps: "+strings.Join(args, " "), func(t *testing.T) {
			if err := runRoot(t, args...); err != nil {
				t.Errorf("autogit %s failed: %v", strings.Join(args, " "), err)
			}
		})
	}
}

func runRoot(t *testing.T, args ...string) error {
	t.Helper()
	root := Root(Version{})
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	return root.ExecuteContext(context.Background())
}
