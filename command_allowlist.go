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
	}

	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List allowlisted jtis",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runAllowlistList,
	}
	addJSONFlag(list)

	add := &cobra.Command{
		Use:   "add <operator> <jti>",
		Short: "Add a jti to the allowlist",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  runAllowlistAdd,
	}

	remove := &cobra.Command{
		Use:   "remove <operator> <jti>",
		Short: "Remove a jti from the allowlist",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  runAllowlistRemove,
	}
	addYesFlag(remove)

	export := &cobra.Command{
		Use:   "export <operator>",
		Short: "Export the allowlist",
		Long: "Export the operator's allowlist in the newline-delimited form " +
			"servers consume (valiss.LoadAllowlistFile): one jti per line, with " +
			"'#' comment lines and blank lines ignored.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runAllowlistExport,
	}

	cmd.AddCommand(list, add, remove, export)
	return cmd
}

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
		out := make([]map[string]string, len(entries))
		for i, e := range entries {
			out[i] = map[string]string{"jti": e.JTI, "added": e.AddedAt.UTC().Format(time.RFC3339)}
		}
		return printJSON(cmd.OutOrStdout(), out)
	}
	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "empty allowlist")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(w, "%s  %s\n", e.JTI, e.AddedAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func runAllowlistAdd(cmd *cobra.Command, args []string) error {
	operator, jti := args[0], args[1]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	present, err := st.HasAllowlist(jti)
	if err != nil {
		return err
	}
	if present {
		fmt.Fprintf(cmd.OutOrStdout(), "%s already allowlisted\n", jti)
		return nil
	}
	if err := st.AddAllowlist(jti); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistAdd, Path: operator, Detail: jti}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added %s to the allowlist\n", jti)
	return nil
}

func runAllowlistRemove(cmd *cobra.Command, args []string) error {
	operator, jti := args[0], args[1]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	present, err := st.HasAllowlist(jti)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("valiss: %s is not in the allowlist", jti)
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Remove %s from the allowlist (revokes it)?", jti))
	if err != nil || !ok {
		return err
	}
	if _, err := st.RemoveAllowlist(jti); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistRemove, Path: operator, Detail: jti}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from the allowlist\n", jti)
	return nil
}

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
	if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistExport, Path: operator,
		Detail: fmt.Sprintf("%d jtis", len(entries))}); err != nil {
		return err
	}
	// The export is exactly what valiss.LoadAllowlistFile consumes: a comment
	// header (ignored on load) followed by one jti per line. The count line
	// doubles as the fleet-convergence marker the operations docs describe.
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "# valiss allowlist for operator %s\n", operator)
	fmt.Fprintf(w, "# count: %d\n", len(entries))
	for _, e := range entries {
		fmt.Fprintln(w, e.JTI)
	}
	return nil
}
