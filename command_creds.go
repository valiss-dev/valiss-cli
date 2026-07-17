package main

import "github.com/spf13/cobra"

// newCredsCommand builds the creds noun: export of the creds artifacts the
// format supports (ADR 0021). The path addresses the entity whose creds are
// exported.
func newCredsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "creds",
		Short: "Export credential artifacts",
	}

	export := &cobra.Command{
		Use:   "export <operator>[/<account>[/<user>]]",
		Short: "Export creds for an entity",
		Long: "Export creds for the addressed entity, covering the four creds " +
			"kinds the format supports.",
		Args: pathArgs(depthOperator, depthUser, 0),
		RunE: stub,
	}
	export.Flags().Bool("bundle", false, "export the full chain as a bundle")
	export.Flags().Bool("bearer", false, "export bearer creds")

	cmd.AddCommand(export)
	return cmd
}
