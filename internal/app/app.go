// Package app orchestrates the three operations. It is the only place where
// git, a provider and a prompt meet.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/preset"
	"github.com/Conte777/autogit/internal/ui"
)

// App holds everything one operation needs.
type App struct {
	repo     *git.Repo
	cfg      *config.Config
	preset   preset.Preset
	provider gen.Provider
	// prompt is the one place that says whether there is anybody to ask.
	// Required: a nil prompt is a wiring bug, not a quiet no.
	prompt ui.Prompter
	// progress is required for the same reason; ui.Noop is the silence.
	progress ui.Progress
}

// New assembles an App. The preset is resolved here, so a broken one is a
// construction error rather than a surprise halfway through a run.
func New(
	repo *git.Repo,
	cfg *config.Config,
	prov gen.Provider,
	prompter ui.Prompter,
	progress ui.Progress,
) (*App, error) {
	p, err := cfg.ResolvePreset()
	if err != nil {
		return nil, err
	}
	return &App{
		repo:     repo,
		cfg:      cfg,
		preset:   p,
		provider: prov,
		prompt:   prompter,
		progress: progress,
	}, nil
}

// The two things a Progress report can be about; there are two Operations.
const (
	commitProgressLabel = "Generating commit message…"
	branchProgressLabel = "Generating branch name…"
)

func (a *App) Root() string { return a.repo.Root() }

// ErrNothingToCommit means the index is empty and nothing was asked to fill it.
var ErrNothingToCommit = errors.New("nothing staged")

// ProtectedBranchError means the branch needs an explicit --force.
type ProtectedBranchError struct {
	Branch string
	Hint   string
}

func (e *ProtectedBranchError) Error() string {
	return fmt.Sprintf("branch %q is protected: %s", e.Branch, e.Hint)
}

// ConsentFunc puts one protected branch to the user and reports their answer.
// It is the second form of the protected-branch override, `--force` being the
// first, and it always resolves to a human.
type ConsentFunc func(ctx context.Context, branch string) (bool, error)

// ConsentError means the user was asked about a protected branch over a
// surface that can reach them, and did not agree.
type ConsentError struct {
	Branch string
}

func (e *ConsentError) Error() string {
	return fmt.Sprintf("the user did not consent to committing on protected branch %q: "+
		"do not repeat this call and do not commit around autogit; ask the user what to do instead",
		e.Branch)
}

// ErrCanceled means the user said no.
var ErrCanceled = errors.New("cancelled")

func (a *App) generate(ctx context.Context, req gen.Request) (gen.Result, error) {
	caller, timeout := ctx, a.cfg.Timeout.Duration()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req.Attempts = a.cfg.Attempts

	result, err := gen.Generate(ctx, a.provider, req)
	// "context deadline exceeded" alone names neither the budget that ran out
	// nor the setting that holds it. Only ours is ours to explain: the caller's
	// deadline is not, and neither is a transport's own, which reports the same
	// error while this context is still running.
	if err != nil && caller.Err() == nil &&
		errors.Is(ctx.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
		return result, fmt.Errorf("%w (%s budget; raise \"timeout\" in the config)", err, timeout)
	}
	return result, err
}
