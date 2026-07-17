package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

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
		Long: "Add a user under an account: generate its key and an " +
			"account-signed user token at the domain's current epoch.",
		Args: pathArgs(depthUser, depthUser, 0),
		RunE: runUserAdd,
	}

	// list is scoped to the addressed account: it lists that account's
	// users, since there is no ambient account context.
	list := &cobra.Command{
		Use:   "list <operator>/<account>",
		Short: "List users under an account",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  runUserList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<account>/<user>",
		Short: "Show a user",
		Args:  pathArgs(depthUser, depthUser, 0),
		RunE:  runUserShow,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<account>/<user>",
		Short: "Remove a user",
		Long: "Remove a user. The user's live tokens are revoked; the blast " +
			"radius is shown before the store is touched.",
		Args: pathArgs(depthUser, depthUser, 0),
		RunE: runUserRemove,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<account>/<user>",
		Short: "Read the user audit journal",
		Long:  "Read the append-only audit journal for the user.",
		Args:  pathArgs(depthUser, depthUser, 0),
		RunE:  runUserAudit,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}

func runUserAdd(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := addUser(st, parentOf(path), childName(path))
	if err != nil {
		return err
	}
	s := summarize(rec)
	fmt.Fprintf(cmd.OutOrStdout(), "Added user %q\n  key: %s\n  epoch: %d\n", s.Path, s.PublicKey, s.Epoch)
	return nil
}

func runUserList(cmd *cobra.Command, args []string) error {
	acctPath := args[0]
	st, err := openStore(operatorOf(acctPath))
	if err != nil {
		return err
	}
	defer st.Close()

	recs, err := st.ListChildren(store.KindUser, acctPath)
	if err != nil {
		return err
	}
	return writeEntityList(cmd, recs)
}

func runUserShow(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.LiveEntity(path)
	if errors.Is(err, store.ErrNoEntity) {
		return fmt.Errorf("valiss: user %q not found", path)
	}
	if err != nil {
		return err
	}
	return writeEntity(cmd, summarize(rec))
}

func runUserRemove(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()
	return removeEntityCmd(cmd, st, path, "user")
}

func runUserAudit(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()
	return writeAudit(cmd, st, path)
}
