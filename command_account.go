package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

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
		Long: "Add an account under an operator: generate its key and an " +
			"operator-signed account token. The account's jti is deposited in " +
			"the allowlist separately, through the allowlist command.",
		Args: pathArgs(depthAccount, depthAccount, 0),
		RunE: runAccountAdd,
	}

	// list is scoped to the addressed operator: it lists that operator's
	// accounts, since there is no ambient operator context.
	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List accounts under an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runAccountList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<account>",
		Short: "Show an account",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  runAccountShow,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<account>",
		Short: "Remove an account",
		Long: "Remove an account. The removal cascades to the account's users " +
			"and revokes their live tokens; the blast radius is shown first.",
		Args: pathArgs(depthAccount, depthAccount, 0),
		RunE: runAccountRemove,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<account>",
		Short: "Read the account audit journal",
		Long:  "Read the append-only audit journal for the account and its users.",
		Args:  pathArgs(depthAccount, depthAccount, 0),
		RunE:  runAccountAudit,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}

func runAccountAdd(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := addAccount(st, operatorOf(path), childName(path))
	if err != nil {
		return err
	}
	s := summarize(rec)
	fmt.Fprintf(cmd.OutOrStdout(), "Added account %q\n  key: %s\n  epoch: %d\n", s.Path, s.PublicKey, s.Epoch)
	return nil
}

func runAccountList(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	recs, err := st.ListChildren(store.KindAccount, operator)
	if err != nil {
		return err
	}
	return writeEntityList(cmd, recs)
}

func runAccountShow(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.LiveEntity(path)
	if errors.Is(err, store.ErrNoEntity) {
		return fmt.Errorf("valiss: account %q not found", path)
	}
	if err != nil {
		return err
	}
	return writeEntity(cmd, summarize(rec))
}

func runAccountRemove(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()
	return removeEntityCmd(cmd, st, path, "account")
}

func runAccountAudit(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()
	return writeAudit(cmd, st, path)
}
