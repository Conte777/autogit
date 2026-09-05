package cli

import (
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
			"the branch prefix.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if g.noInput {
				out.SetInteractive(false)
			}

			a, err := build(cmd.Context(), g, app.SurfaceCLI, out)
			if err != nil {
				return err
			}
			req := app.ParseBranchArgs(args, a.Preset.Branch)
			if ticket != "" {
				req.Ticket = ticket
			}
			result, err := a.Branch(cmd.Context(), req)
			if err != nil {
				return err
			}
			out.Print("switched to new branch %s", result.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&ticket, "ticket", "", "ticket id to use as the branch prefix")
	return cmd
}
