package main

import "github.com/spf13/cobra"

// newUserCommand builds the user noun. Users are store entities addressed
// as <operator>/<account>/<user> (ADR 0021).
func newUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
		Long:  "Manage users, the leaves of a valiss signing chain.",
	}

	add := &cobra.Command{
		Use:   "add <operator>/<account>/<user>",
		Short: "Add a user",
		Args:  pathArgs(depthUser, depthUser, 0),
		RunE:  stub,
	}

	// list is scoped to the addressed account: it lists that account's
	// users, since there is no ambient account context.
	list := &cobra.Command{
		Use:   "list <operator>/<account>",
		Short: "List users under an account",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  stub,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<account>/<user>",
		Short: "Show a user",
		Args:  pathArgs(depthUser, depthUser, 0),
		RunE:  stub,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<account>/<user>",
		Short: "Remove a user",
		Long: "Remove a user. The user's live tokens are revoked; the blast " +
			"radius is shown before the store is touched.",
		Args: pathArgs(depthUser, depthUser, 0),
		RunE: stub,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<account>/<user>",
		Short: "Read the user audit journal",
		Long:  "Read the append-only audit journal for the user.",
		Args:  pathArgs(depthUser, depthUser, 0),
		RunE:  stub,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}
