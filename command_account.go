package main

import "github.com/spf13/cobra"

// newAccountCommand builds the account noun. Accounts are store entities
// addressed as <operator>/<account> (ADR 0021).
func newAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage accounts",
		Long:  "Manage accounts, the second tier of a valiss signing chain.",
	}

	add := &cobra.Command{
		Use:   "add <operator>/<account>",
		Short: "Add an account",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  stub,
	}

	// list is scoped to the addressed operator: it lists that operator's
	// accounts, since there is no ambient operator context.
	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List accounts under an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<account>",
		Short: "Show an account",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  stub,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<account>",
		Short: "Remove an account",
		Long: "Remove an account. The removal cascades to the account's users " +
			"and revokes their live tokens; the blast radius is shown first.",
		Args: pathArgs(depthAccount, depthAccount, 0),
		RunE: stub,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<account>",
		Short: "Read the account audit journal",
		Long:  "Read the append-only audit journal for the account and its users.",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  stub,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}
