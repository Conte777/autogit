package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/ui"
)

func openRepo(ctx context.Context, path string, surface app.Surface) (*git.Repo, error) {
	if path == "" {
		path = "."
	}
	return git.Open(ctx, path, git.Options{Interactive: surface == app.SurfaceCLI})
}

func commitCmd(g *globals, out *ui.UI) *cobra.Command {
	var (
		all     bool
		tracked bool
		force   bool
		dryRun  bool
	)

	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit the staged changes with a generated message",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && tracked {
				return &usageError{errors.New("--all and --tracked are mutually exclusive")}
			}
			req := app.CommitRequest{
				Stage:   app.StageModeFor(all, tracked),
				Force:   force,
				Preview: dryRun,
				NoInput: g.noInput,
			}
			return runCommit(cmd.Context(), g, out, req)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "stage everything first, including untracked files")
	cmd.Flags().BoolVar(&tracked, "tracked", false, "stage tracked files first")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "allow a protected branch")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the message without committing")
	return cmd
}

func commitMsgCmd(g *globals, out *ui.UI) *cobra.Command {
	return &cobra.Command{
		Use:   "commit-msg",
		Short: "Print the message that `commit` would use, without committing",
		Long: "commit-msg runs the exact code path `commit` runs and stops before the\n" +
			"commit, so the preview cannot differ from what would land.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCommit(cmd.Context(), g, out, app.CommitRequest{
				Stage:   app.StageStaged,
				Preview: true,
				NoInput: g.noInput,
			})
		},
	}
}

func runCommit(ctx context.Context, g *globals, out *ui.UI, req app.CommitRequest) error {
	if g.noInput {
		out.SetInteractive(false)
	}
	a, err := build(ctx, g, app.SurfaceCLI, out)
	if err != nil {
		return err
	}

	result, err := a.Commit(ctx, req)
	if err != nil {
		return err
	}
	out.Print("%s", result.Summary(app.SummaryHuman))
	// A preview keeps the note on stderr so that `autogit commit-msg > file`
	// gets the message and nothing else; the committed line carries it inline.
	if result.Preview && result.Prepared != git.OpNone {
		out.Warn("this is git's own %s message, used verbatim", result.Prepared)
	}
	return nil
}
