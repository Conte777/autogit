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

// Surface is where the request came from. It decides whether questions may be
// asked at all, and whether `confirm` applies.
type Surface string

const (
	SurfaceCLI  Surface = "cli"
	SurfaceMCP  Surface = "mcp"
	SurfaceHook Surface = "hook"
)

// App holds everything one operation needs.
type App struct {
	Repo     *git.Repo
	Config   *config.Config
	Preset   preset.Preset
	Provider gen.Provider
	Prompt   ui.Prompter
	Surface  Surface
	// PresetName selects the embedded prompt when the preset points at no file.
	PresetName string
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

// interactive reports whether this surface may ask the user anything.
func (a *App) interactive() bool {
	return a.Surface == SurfaceCLI && a.Prompt != nil && a.Prompt.Interactive()
}

func (a *App) generate(ctx context.Context, req gen.Request) (gen.Result, error) {
	if timeout := a.Config.Timeout.Duration(); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req.Attempts = a.Config.Attempts
	return gen.Generate(ctx, a.Provider, req)
}
