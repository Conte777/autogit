package cli

import (
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/app"
	"github.com/Conte777/autogit/internal/ui"
)

// ticketArg matches a leading argument that is only a ticket id.
var ticketArg = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)

func branchCmd(g *globals, out *ui.UI) *cobra.Command {
	var ticket string

	cmd := &cobra.Command{
		Use:   "branch [TICKET] [description...]",
		Short: "Create and switch to a new branch named <prefix>/<slug>",
		Long: "With a description the slug comes from it verbatim; without one the slug\n" +
			"is derived from the uncommitted diff. A leading TICKET argument becomes\n" +
			"the branch prefix.",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, desc := splitTicket(args)
			if ticket != "" {
				t = ticket
			}
			a, err := build(cmd.Context(), g, prompterFor(g, out))
			if err != nil {
				return err
			}
			result, err := a.Branch(cmd.Context(), app.BranchRequest{Ticket: t, Description: desc})
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

// splitTicket peels a leading ticket argument off the free text.
func splitTicket(args []string) (ticket, description string) {
	if len(args) > 0 && ticketArg.MatchString(args[0]) {
		return strings.ToUpper(args[0]), strings.Join(args[1:], " ")
	}
	return "", strings.Join(args, " ")
}
