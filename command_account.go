package main

import (
	"errors"
	"fmt"
	"time"

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
			"operator-signed account token. The account token's jti is the key " +
			"a server's allowlist consults, so it is deposited in the local " +
			"allowlist by default; pass --no-allowlist to opt out (for minting " +
			"into a domain whose allowlist lives elsewhere).",
		Args: pathArgs(depthAccount, depthAccount, 0),
		RunE: runAccountAdd,
	}
	add.Flags().Bool("no-allowlist", false, "do not deposit the account jti in the local allowlist")

	// list is scoped to the addressed operator: it lists that operator's
	// accounts, since there is no ambient operator context.
	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List accounts under an operator",
		Long:  "List the live accounts under an operator (addressed as <operator>): one per line with public key, generation, and epoch.",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runAccountList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<account>",
		Short: "Show an account",
		Long:  "Show one account (addressed as <operator>/<account>): its kind, path, public key, generation, epoch, and token jti.",
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
	requireSubcommand(cmd)
	return cmd
}

func runAccountAdd(cmd *cobra.Command, args []string) error {
	path := args[0]
	noAllowlist, err := cmd.Flags().GetBool("no-allowlist")
	if err != nil {
		return err
	}
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
	registered := false
	if !noAllowlist {
		if registered, err = st.AddAllowlist(s.JTI, time.Now().UTC()); err != nil {
			return err
		}
		if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistAdd, Path: path,
			Detail: fmt.Sprintf("jti=%s (account add)", s.JTI)}); err != nil {
			return err
		}
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Added account %q\n  public key: %s\n  epoch: %d\n  jti: %s\n", s.Path, s.PublicKey, s.Epoch, s.JTI)
	switch {
	case noAllowlist:
		fmt.Fprintln(w, "  allowlist: skipped (--no-allowlist)")
	case registered:
		fmt.Fprintln(w, "  allowlist: registered")
	default:
		fmt.Fprintln(w, "  allowlist: already present")
	}
	fmt.Fprintf(w, "  next: valiss user add %s/<user>\n", s.Path)
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
