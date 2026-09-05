package cli

import (
	"context"
	"errors"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/gen"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/ui"
)

// Exit codes. They are part of the interface: scripts and the Claude Code hook
// branch on them.
const (
	ExitOK         = 0
	ExitInternal   = 1
	ExitUsage      = 2
	ExitRepo       = 3
	ExitNothing    = 4
	ExitProtected  = 5
	ExitProvider   = 6
	ExitValidation = 7
	ExitConfig     = 8
	ExitCanceled   = 130
)

// usageError marks a bad invocation, which exits 2 rather than 1.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// ExitCode maps an error onto its exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}

	var (
		usage   *usageError
		state   *git.StateError
		execErr *git.ExecError
		cfgErr  *config.Error
		protErr *app.ProtectedBranchError
		provErr *gen.ProviderError
		failErr *gen.FailureError
	)

	// Cancellation is answered before any classification: it says the process was
	// interrupted, not which layer failed. It is also the one stdlib sentinel
	// matched through a wrapper, because every layer propagates it by convention.
	switch {
	case errors.As(err, &usage):
		return ExitUsage
	case errors.Is(err, context.Canceled), errors.Is(err, app.ErrCanceled):
		return ExitCanceled
	case errors.As(err, &cfgErr):
		return ExitConfig
	case errors.Is(err, git.ErrNotARepo), errors.As(err, &state), errors.As(err, &execErr):
		return ExitRepo
	case app.IsNothingToCommit(err), errors.Is(err, app.ErrNoBranchInput):
		return ExitNothing
	case errors.As(err, &protErr):
		return ExitProtected
	case errors.As(err, &provErr):
		return ExitProvider
	case errors.As(err, &failErr):
		return ExitValidation
	case errors.Is(err, ui.ErrNoInput):
		// A question with nobody to answer it is a usage problem: the caller
		// should have passed the flag instead.
		return ExitUsage
	default:
		return ExitInternal
	}
}

// Detail returns the extra lines worth showing for an error, or "".
func Detail(err error) string {
	var failErr *gen.FailureError
	if errors.As(err, &failErr) && failErr.Last != "" {
		return "last candidate: " + failErr.Last
	}
	return ""
}
