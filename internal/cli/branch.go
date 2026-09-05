package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/ui"
)

func branchCmd(g *globals, out *ui.UI) *cobra.Command {
	var ticket string

	cmd := &cobra.Command{
		Use:   "branch [TICKET] [description...]",
		Short: "Create and switch to a new branch named <prefix>/<slug>",
		Long: "With a description the slug comes from it verbatim; without one the slug\n" +
			"is derived from the uncommitted diff. A leading TICKET argument becomes\n" +
			"the branch prefix when it matches the preset's ticket pattern; anything\n" +
			"else is description text.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := build(cmd.Context(), g, prompterFor(g, out))
			if err != nil {
				return err
			}
			req := app.BranchRequest{Ticket: ticket, Description: strings.Join(args, " ")}
			if ticket == "" {
				req = a.ParseBranchArgs(args)
			}
			result, err := a.Branch(cmd.Context(), req)
			if err != nil {
				return err
			}
			out.Print("%s", result.Summary())
			return nil
		},
	}

	cmd.Flags().StringVar(&ticket, "ticket", "", "ticket id to use as the branch prefix; the arguments are then all description")
	return cmd
}
