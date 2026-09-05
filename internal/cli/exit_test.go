package cli_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/cli"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/ui"
)

// execFailure is what a git command that ran and refused leaves behind: the
// error cmd.Run() returns, which is what git.run puts in ExecError.Err.
func execFailure(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("want a failing command")
	}
	return err
}

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
		{
			"git refused",
			&git.ExecError{Args: []string{"commit"}, Stderr: "hook declined", Err: execFailure(t)},
			cli.ExitRepo,
		},
		{
			"git timed out",
			&git.ExecError{Args: []string{"commit"}, Err: fmt.Errorf("%w (30s)", context.DeadlineExceeded)},
			cli.ExitRepo,
		},
		{
			"git cancelled",
			&git.ExecError{Args: []string{"commit"}, Err: fmt.Errorf("%w (30s)", context.Canceled)},
			cli.ExitCanceled,
		},
		{"nothing staged", app.ErrNothingToCommit, cli.ExitNothing},
		{"nothing staged, wrapped", fmt.Errorf("%w: hint", app.ErrNothingToCommit), cli.ExitNothing},
		{"no branch input", app.ErrNoBranchInput, cli.ExitNothing},
		{"protected", &app.ProtectedBranchError{Branch: "main"}, cli.ExitProtected},
		{"provider", &gen.ProviderError{Provider: "claude-cli", Op: "start", Err: errors.New("no binary")}, cli.ExitProvider},
		{
			"provider timed out",
			&gen.ProviderError{Provider: "claude-cli", Op: "send", Err: fmt.Errorf("post: %w", context.DeadlineExceeded)},
			cli.ExitProvider,
		},
		{
			"provider cancelled",
			&gen.ProviderError{Provider: "claude-cli", Op: "send", Err: fmt.Errorf("post: %w", context.Canceled)},
			cli.ExitCanceled,
		},
		{"validation", &gen.FailureError{Attempts: 3, Last: "nope"}, cli.ExitValidation},
		{"config", &config.Error{Err: errors.New("unknown key")}, cli.ExitConfig},
		{"unbuildable provider", &config.Error{Err: errors.New("no API key: set ANTHROPIC_API_KEY")}, cli.ExitConfig},
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

func TestDetailShowsTheLastCandidate(t *testing.T) {
	err := &gen.FailureError{Attempts: 3, Last: "Feat: Add Thing", Problems: []string{"lowercase"}}
	if got := cli.Detail(err); !strings.Contains(got, "Feat: Add Thing") {
		t.Errorf("Detail() = %q, want the rejected candidate", got)
	}
	if got := cli.Detail(errors.New("plain")); got != "" {
		t.Errorf("Detail() = %q, want empty for an ordinary error", got)
	}
}
