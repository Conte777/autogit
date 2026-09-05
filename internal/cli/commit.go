package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/git"
	"github.com/Conte777/autogit/internal/ui"
)

// openRepo gives git the same permission to ask that autogit has.
func openRepo(ctx context.Context, path string, prompter ui.Prompter) (*git.Repo, error) {
	if path == "" {
		path = "."
	}
	return git.Open(ctx, path, git.Options{Interactive: prompter.Interactive()})
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
				Stage:   stageMode(all, tracked),
				Force:   force,
				Preview: dryRun,
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
			})
		},
	}
}

func stageMode(all, tracked bool) app.StageMode {
	switch {
	case all:
		return app.StageAll
	case tracked:
		return app.StageTracked
	default:
		return app.StageStaged
	}
}

func runCommit(ctx context.Context, g *globals, out *ui.UI, req app.CommitRequest) error {
	a, err := build(ctx, g, prompterFor(g, out))
	if err != nil {
		return err
	}

	result, err := a.Commit(ctx, req)
	if err != nil {
		return err
	}
	if result.Preview {
		out.Print("%s", result.Message)
		// The note goes to stderr so that `autogit commit-msg > file` keeps the
		// message and nothing else.
		if result.Prepared != git.OpNone {
			out.Warn("this is git's own %s message, used verbatim", result.Prepared)
		}
		return nil
	}
	line := fmt.Sprintf("committed %s: %s", result.ShortHash, firstLine(result.Message))
	if result.Prepared != git.OpNone {
		line += fmt.Sprintf(" (git's own %s message)", result.Prepared)
	}
	out.Print("%s", line)
	return nil
}

func firstLine(s string) string {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
