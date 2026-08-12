package cli

import (
	"github.com/spf13/cobra"

	"github.com/Conte777/autogit/internal/config"
	"github.com/Conte777/autogit/internal/ui"
)

func schemaCmd(out *ui.UI) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON schema of the config file",
		Long: "The schema is generated from the Go types, so `autogit schema | diff -`\n" +
			"against schema/config.schema.json is what keeps the two in step.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			data, err := config.Schema()
			if err != nil {
				return err
			}
			out.Raw(string(data))
			return nil
		},
	}
}
