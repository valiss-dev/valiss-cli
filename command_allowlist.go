package main

import "github.com/spf13/cobra"

// newAllowlistCommand builds the allowlist noun. The allowlist is local to
// an operator's store; export produces exactly what servers consume
// (ADR 0021).
func newAllowlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowlist",
		Short: "Manage the local jti allowlist",
	}

	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List allowlisted jtis",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(list)

	add := &cobra.Command{
		Use:   "add <operator> <jti>",
		Short: "Add a jti to the allowlist",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  stub,
	}

	remove := &cobra.Command{
		Use:   "remove <operator> <jti>",
		Short: "Remove a jti from the allowlist",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  stub,
	}
	addYesFlag(remove)

	export := &cobra.Command{
		Use:   "export <operator>",
		Short: "Export the allowlist",
		Long:  "Export the operator's allowlist in the form servers consume.",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}

	cmd.AddCommand(list, add, remove, export)
	return cmd
}
