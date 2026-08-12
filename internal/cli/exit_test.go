package cli_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/cli"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/ui"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, cli.ExitOK},
		{"unknown", errors.New("something broke"), cli.ExitInternal},
		{"not a repo", fmt.Errorf("%w: /tmp/x", git.ErrNotARepo), cli.ExitRepo},
		{"repo state", &git.StateError{Reason: "a merge is in progress"}, cli.ExitRepo},
		{"nothing staged", app.ErrNothingToCommit, cli.ExitNothing},
		{"nothing staged, wrapped", fmt.Errorf("%w: hint", app.ErrNothingToCommit), cli.ExitNothing},
		{"no branch input", app.ErrNoBranchInput, cli.ExitNothing},
		{"protected", &app.ProtectedBranchError{Branch: "main"}, cli.ExitProtected},
		{"provider", &gen.ProviderError{Provider: "claude-cli", Err: errors.New("no binary")}, cli.ExitProvider},
		{"timeout", context.DeadlineExceeded, cli.ExitProvider},
		{"validation", &gen.FailureError{Attempts: 3, Last: "nope"}, cli.ExitValidation},
		{"config", &config.Error{Err: errors.New("unknown key")}, cli.ExitConfig},
		{"cancelled", app.ErrCanceled, cli.ExitCanceled},
		{"ctx cancelled", context.Canceled, cli.ExitCanceled},
		{"no terminal", ui.ErrNoInput, cli.ExitUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cli.ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// A git failure wrapping a deadline must read as a provider-class timeout, not
// as an internal error: that is the hanging-signer case.
func TestExitCodeForATimedOutGitCall(t *testing.T) {
	err := &git.ExecError{
		Args:   []string{"commit"},
		Stderr: "",
		Err:    fmt.Errorf("%w (30s)", context.DeadlineExceeded),
	}
	if got := cli.ExitCode(err); got != cli.ExitProvider {
		t.Errorf("ExitCode() = %d, want %d", got, cli.ExitProvider)
	}
}

func TestDetailShowsTheLastCandidate(t *testing.T) {
	err := &gen.FailureError{Attempts: 3, Last: "Feat: Add Thing", Problems: []string{"lowercase"}}
	if got := cli.Detail(err); !strings.Contains(got, "Feat: Add Thing") {
		t.Errorf("Detail() = %q, want the rejected candidate", got)
	}
	if got := cli.Detail(errors.New("plain")); got != "" {
		t.Errorf("Detail() = %q, want empty for an ordinary error", got)
	}
}
