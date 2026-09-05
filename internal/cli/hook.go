package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/hook"
	"github.com/Conte777/autogit/internal/ui"
)

func hookCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "hook",
		Short: "Claude Code UserPromptSubmit hook (reads the payload on stdin)",
		Long: "Blocks the prompt for /commit, /commit-msg and /branch and does the work\n" +
			"itself, so the model never wakes up. Any other prompt passes through.",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return hook.Run(cmd.Context(), os.Stdin, os.Stdout, os.LookupEnv,
				func(ctx context.Context, c hook.Command) (string, error) {
					return runHookCommand(ctx, g, c)
				})
		},
	}
}

func runHookCommand(ctx context.Context, g *globals, c hook.Command) (string, error) {
	// No terminal on this surface: questions become errors carrying the exact
	// command to retype.
	g.noInput = true
	a, err := build(ctx, g, app.SurfaceHook, ui.Noop{})
	if err != nil {
		return "", err
	}

	switch c.Kind {
	case hook.KindBranch:
		result, err := a.Branch(ctx, app.ParseBranchArgs(c.Args, a.Preset.Branch))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("switched to new branch %s", result.Name), nil

	default:
		result, err := a.Commit(ctx, app.CommitRequest{
			Stage:   app.StageModeFor(c.All, c.Tracked),
			Force:   c.Force,
			Preview: c.DryRun || c.Kind == hook.KindCommitMsg,
			NoInput: true,
		})
		if err != nil {
			return "", err
		}
		// The label matters most here: the model is told this tool produces
		// conventional commits, and would otherwise try to "fix" a subject git
		// wrote.
		if result.Preview {
			return result.Message + preparedSuffix(result.Prepared, true), nil
		}
		return fmt.Sprintf("committed %s: %s%s",
			result.ShortHash, firstLine(result.Message), preparedSuffix(result.Prepared, false)), nil
	}
}

func preparedSuffix(op git.Operation, preview bool) string {
	switch {
	case op == git.OpNone:
		return ""
	case preview:
		return fmt.Sprintf("\n\n(git's own %s message; it would be used verbatim, not generated)", op)
	default:
		return fmt.Sprintf(" (git's own %s message, used verbatim)", op)
	}
}
