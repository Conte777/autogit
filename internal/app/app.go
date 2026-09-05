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
}

// New assembles an App. The preset is resolved here, so a broken one is a
// construction error rather than a surprise halfway through a run.
func New(repo *git.Repo, cfg *config.Config, prov gen.Provider, prompter ui.Prompter) (*App, error) {
	p, err := cfg.ResolvePreset()
	if err != nil {
		return nil, err
	}
	return &App{repo: repo, cfg: cfg, preset: p, provider: prov, prompt: prompter}, nil
}

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

// ErrCanceled means the user said no.
var ErrCanceled = errors.New("cancelled")

func (a *App) generate(ctx context.Context, req gen.Request) (gen.Result, error) {
	if timeout := a.cfg.Timeout.Duration(); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req.Attempts = a.cfg.Attempts
	return gen.Generate(ctx, a.provider, req)
}
