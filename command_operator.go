package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

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
		Long: "Add an operator: generate its signing key and self-signed " +
			"operator token, and record it as the root of a new signing chain. " +
			"The operator's store is created (with default configuration) if it " +
			"does not exist yet; use 'store init' first to set a non-default " +
			"audit-retention window.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runOperatorAdd,
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List operators",
		Long:  "List the operators with a store under the store directory.",
		Args:  cobra.NoArgs,
		RunE:  runOperatorList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>",
		Short: "Show an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runOperatorShow,
	}
	addJSONFlag(show)

	token := &cobra.Command{
		Use:   "token <operator>",
		Short: "Print the operator's self-signed token",
		Long: "Print the operator's self-signed token, the trust domain's policy " +
			"statement (epoch and validity window). It is what a server pins with " +
			"valiss-go's Verifier WithOperatorToken to enforce epoch rotation; the " +
			"operator public key remains the trust anchor. The token carries no " +
			"seed and is safe to distribute.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runOperatorToken,
	}

	rotate := &cobra.Command{
		Use:   "rotate <operator>",
		Short: "Rotate an operator signing key",
		Long: "Rotate an operator by advancing its epoch and re-minting the " +
			"self-signed operator token. Verifiers that adopt the new operator " +
			"token stop accepting account and user tokens at the old epoch, so " +
			"the whole domain rotates once the new token is distributed. The " +
			"operator key (the pinned trust anchor) is kept.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runOperatorRotate,
	}
	addYesFlag(rotate)

	remove := &cobra.Command{
		Use:   "remove <operator>",
		Short: "Remove an operator",
		Long: "Remove an operator. The removal cascades: descendant accounts " +
			"and users fall with it and their live tokens are revoked. The " +
			"blast radius is shown before the store is touched.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runOperatorRemove,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>",
		Short: "Read the operator audit journal",
		Long:  "Read the append-only audit journal for the operator and its whole subtree.",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runOperatorAudit,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, token, rotate, remove, audit)
	return cmd
}

// runOperatorToken prints the operator's self-signed token blob, so it can be
// handed to a server's WithOperatorToken.
func runOperatorToken(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.LiveEntity(operator)
	if errors.Is(err, store.ErrNoEntity) {
		return errNoOperator
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), rec.Token)
	return nil
}

// runOperatorAdd creates the operator identity, auto-initializing the store
// with default configuration when it does not exist yet.
func runOperatorAdd(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openOrInitStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := addOperator(st, operator)
	if err != nil {
		return err
	}
	s := summarize(rec)
	fmt.Fprintf(cmd.OutOrStdout(), "Added operator %q\n  key: %s\n  epoch: %d\n", s.Name, s.PublicKey, s.Epoch)
	return nil
}

// runOperatorList lists the operators that have a store under the store
// directory.
func runOperatorList(cmd *cobra.Command, args []string) error {
	dir, err := storeDir()
	if err != nil {
		return err
	}
	names, err := listStoreOperators(dir)
	if err != nil {
		return err
	}
	var summaries []entitySummary
	for _, name := range names {
		st, err := openStore(name)
		if err != nil {
			return err
		}
		rec, err := st.LiveEntity(name)
		st.Close()
		if errors.Is(err, store.ErrNoEntity) {
			// A store initialized but with no operator identity yet.
			continue
		}
		if err != nil {
			return err
		}
		summaries = append(summaries, summarize(rec))
	}

	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), summaries)
	}
	if len(summaries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no operators")
		return nil
	}
	for _, s := range summaries {
		fmt.Fprintf(cmd.OutOrStdout(), "%-24s %s  gen=%d epoch=%d\n", s.Name, s.PublicKey, s.Generation, s.Epoch)
	}
	return nil
}

// runOperatorShow prints one operator's details.
func runOperatorShow(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.LiveEntity(operator)
	if errors.Is(err, store.ErrNoEntity) {
		return errNoOperator
	}
	if err != nil {
		return err
	}
	return writeEntity(cmd, summarize(rec))
}

// runOperatorRotate advances the operator's epoch after confirmation.
func runOperatorRotate(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	if _, err := st.LiveEntity(operator); errors.Is(err, store.ErrNoEntity) {
		return errNoOperator
	} else if err != nil {
		return err
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Rotate operator %q (advance its epoch and re-issue all accounts and users at the new epoch)?", operator))
	if err != nil || !ok {
		return err
	}
	res, err := rotateOperator(st)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Rotated operator %q to epoch %d\n", operator, res.operator.Epoch)
	fmt.Fprintf(w, "  re-issued: %d account(s), %d user(s) at epoch %d\n", res.accounts, res.users, res.operator.Epoch)
	fmt.Fprintln(w, "  allowlist: account entries swapped to the new jtis")
	fmt.Fprintln(w, "  next: re-export creds and the allowlist; distribute the new operator token (operator token)")
	return nil
}

// runOperatorRemove removes the operator after showing the blast radius and
// confirming.
func runOperatorRemove(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	fallen, tokens, err := blastRadius(st, operator)
	if err != nil {
		return err
	}
	if len(fallen) == 0 {
		return errNoOperator
	}
	printBlastRadius(cmd, fallen, tokens)
	ok, err := confirmed(cmd, fmt.Sprintf("Remove operator %q and everything above?", operator))
	if err != nil || !ok {
		return err
	}
	if _, _, err := removeEntity(st, operator); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed operator %q (%d entities, %d tokens revoked)\n", operator, len(fallen), tokens)
	return nil
}

// runOperatorAudit reads the operator's whole-subtree audit journal.
func runOperatorAudit(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()
	return writeAudit(cmd, st, operator)
}
