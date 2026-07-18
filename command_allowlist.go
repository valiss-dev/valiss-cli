package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

// newAllowlistCommand builds the allowlist noun. The allowlist is local to
// an operator's store; export produces exactly what servers consume
// (ADR 0021).
func newAllowlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allowlist",
		Short: "Manage the local jti allowlist",
		Long: "Manage the operator's local jti allowlist, the fail-closed " +
			"revocation surface. It keys on account jtis (an account add deposits " +
			"its jti here; a token revoke removes it), and export emits exactly " +
			"what valiss-go's LoadAllowlistFile consumes: the newline-delimited " +
			"account jtis a server admits.",
	}

	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List allowlisted jtis",
		Long:  "List the operator's allowlisted jtis (account jtis), one per line.",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runAllowlistList,
	}
	addJSONFlag(list)

	add := &cobra.Command{
		Use:   "add <operator> <jti>",
		Short: "Add a jti to the allowlist",
		Long: "Add an account jti to the operator's allowlist, admitting it (and " +
			"every user beneath its account) at any server that loads the exported " +
			"allowlist. Adding a jti already present is a no-op.",
		Args: pathArgs(depthOperator, depthOperator, 1),
		RunE: runAllowlistAdd,
	}

	remove := &cobra.Command{
		Use:   "remove <operator> <jti>",
		Short: "Remove a jti from the allowlist",
		Long: "Remove an account jti from the operator's allowlist, cutting it (and " +
			"every user beneath its account) at any server that loads the exported " +
			"allowlist. Removing a jti already absent is a no-op.",
		Args: pathArgs(depthOperator, depthOperator, 1),
		RunE: runAllowlistRemove,
	}
	addYesFlag(remove)

	export := &cobra.Command{
		Use:   "export <operator>",
		Short: "Export the allowlist",
		Long:  "Export the operator's allowlist in the newline-delimited form servers load (valiss-go LoadAllowlistFile).",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runAllowlistExport,
	}

	cmd.AddCommand(list, add, remove, export)
	requireSubcommand(cmd)
	return cmd
}

// allowlistEntryJSON is the JSON shape of one allowlist entry.
type allowlistEntryJSON struct {
	JTI   string `json:"jti"`
	Added string `json:"added,omitempty"`
}

// runAllowlistList lists the operator's allowlisted jtis.
func runAllowlistList(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	entries, err := st.ListAllowlist()
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		out := make([]allowlistEntryJSON, len(entries))
		for i, e := range entries {
			out[i] = allowlistEntryJSON{JTI: e.JTI, Added: e.AddedAt.UTC().Format(time.RFC3339)}
		}
		return printJSON(cmd.OutOrStdout(), out)
	}
	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "none")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintln(w, e.JTI)
	}
	return nil
}

// runAllowlistAdd deposits a jti in the allowlist.
func runAllowlistAdd(cmd *cobra.Command, args []string) error {
	operator, jti := args[0], args[1]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	added, err := st.AddAllowlist(jti, time.Now().UTC())
	if err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(cmd.OutOrStdout(), "jti %q already in allowlist\n", jti)
		return nil
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistAdd, Path: operator,
		Detail: fmt.Sprintf("jti=%s", jti)}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added jti %q to the allowlist\n", jti)
	return nil
}

// runAllowlistRemove removes a jti from the allowlist after confirmation.
func runAllowlistRemove(cmd *cobra.Command, args []string) error {
	operator, jti := args[0], args[1]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	present, err := st.AllowlistContains(jti)
	if err != nil {
		return err
	}
	if !present {
		// Idempotent, mirroring allowlist add: an absent jti is already in the
		// desired state, so removing it is a no-op success rather than an error.
		fmt.Fprintf(cmd.OutOrStdout(), "jti %q not in allowlist\n", jti)
		return nil
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Remove jti %q from the allowlist?", jti))
	if err != nil || !ok {
		return err
	}
	if _, err := st.RemoveAllowlist(jti); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistRemove, Path: operator,
		Detail: fmt.Sprintf("jti=%s", jti)}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed jti %q from the allowlist\n", jti)
	return nil
}

// runAllowlistExport writes the allowlist in the newline-delimited form a
// server loads: a header comment, then one jti per line. Comment and blank
// lines are ignored by valiss-go's LoadAllowlistFile.
func runAllowlistExport(cmd *cobra.Command, args []string) error {
	operator := args[0]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	entries, err := st.ListAllowlist()
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "# valiss allowlist for operator %s\n", operator)
	fmt.Fprintf(w, "# generated %s (%d jti%s)\n", time.Now().UTC().Format(time.RFC3339), len(entries), plural(len(entries), "", "s"))
	for _, e := range entries {
		fmt.Fprintln(w, e.JTI)
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistExport, Path: operator,
		Detail: fmt.Sprintf("%d jtis", len(entries))}); err != nil {
		return err
	}
	return nil
}
