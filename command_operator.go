package main

import "github.com/spf13/cobra"

// newOperatorCommand builds the operator noun. Operators are store entities
// and take the entity verb set, plus rotate for key rotation (ADR 0021).
func newOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Manage operators",
		Long:  "Manage operators, the roots of a valiss signing chain.",
	}

	add := &cobra.Command{
		Use:   "add <operator>",
		Short: "Add an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List operators",
		Args:  cobra.NoArgs,
		RunE:  stub,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>",
		Short: "Show an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(show)

	rotate := &cobra.Command{
		Use:   "rotate <operator>",
		Short: "Rotate an operator signing key",
		Long: "Rotate an operator signing key. Rotation bumps the operator " +
			"generation and invalidates tokens signed by the retired key.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: stub,
	}
	addYesFlag(rotate)

	remove := &cobra.Command{
		Use:   "remove <operator>",
		Short: "Remove an operator",
		Long: "Remove an operator. The removal cascades: descendant accounts " +
			"and users fall with it and their live tokens are revoked. The " +
			"blast radius is shown before the store is touched.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: stub,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>",
		Short: "Read the operator audit journal",
		Long:  "Read the append-only audit journal for the operator and its whole subtree.",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, rotate, remove, audit)
	return cmd
}
