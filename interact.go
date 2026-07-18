package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

// errAborted reports a destructive command declined at the confirmation prompt
// (an explicit "n", or EOF on a non-interactive stdin). It is a real error so
// the process exits non-zero: a scripted `revoke ... && echo done` must not
// treat a decline as success.
var errAborted = errors.New("valiss: aborted")

// confirmed resolves a destructive command's go/no-go. With --yes it returns
// true without prompting (the scripted path). Otherwise it prints the prompt
// and reads a yes/no answer from the command's input; a non-yes answer returns
// errAborted, so the caller aborts and the process exits non-zero. The prompt
// goes to stderr, keeping stdout a clean data channel.
func confirmed(cmd *cobra.Command, prompt string) (bool, error) {
	yes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return false, err
	}
	if yes {
		return true, nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N]: ", prompt)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("valiss: reading confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, errAborted
	}
}

// childName returns the last segment of an entity path (the entity's own name
// within its parent).
func childName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// parentOf returns an entity path with its last segment removed (the parent
// path); it returns "" for a single-segment path.
func parentOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return ""
}

// writeEntityList renders a list of entities as text or JSON.
func writeEntityList(cmd *cobra.Command, recs []store.EntityRecord) error {
	summaries := make([]entitySummary, 0, len(recs))
	for _, r := range recs {
		summaries = append(summaries, summarize(r))
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), summaries)
	}
	w := cmd.OutOrStdout()
	if len(summaries) == 0 {
		fmt.Fprintln(w, "none")
		return nil
	}
	for _, s := range summaries {
		fmt.Fprintf(w, "%-32s %s  gen=%d epoch=%d\n", s.Path, s.PublicKey, s.Generation, s.Epoch)
	}
	return nil
}

// removeEntityCmd runs the shared remove flow for a non-operator entity: show
// the blast radius, confirm, tombstone the subtree and revoke its live tokens.
func removeEntityCmd(cmd *cobra.Command, st *store.Local, path, kindLabel string) error {
	fallen, tokens, err := blastRadius(st, path)
	if err != nil {
		return err
	}
	if len(fallen) == 0 {
		return fmt.Errorf("valiss: %s %q not found", kindLabel, path)
	}
	printBlastRadius(cmd, fallen, tokens)
	ok, err := confirmed(cmd, fmt.Sprintf("Remove %s %q and everything under it?", kindLabel, path))
	if err != nil || !ok {
		return err
	}
	if _, _, err := removeEntity(st, path); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s %q (%d entit%s, %d token%s revoked)\n",
		kindLabel, path, len(fallen), plural(len(fallen), "y", "ies"), tokens, plural(tokens, "", "s"))
	return nil
}

// writeEntity renders one entity summary as text or JSON.
func writeEntity(cmd *cobra.Command, s entitySummary) error {
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), s)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-12s %s\n", "kind:", s.Kind)
	fmt.Fprintf(w, "%-12s %s\n", "path:", s.Path)
	fmt.Fprintf(w, "%-12s %s\n", "name:", s.Name)
	fmt.Fprintf(w, "%-12s %s\n", "public key:", s.PublicKey)
	fmt.Fprintf(w, "%-12s %d\n", "generation:", s.Generation)
	fmt.Fprintf(w, "%-12s %d\n", "epoch:", s.Epoch)
	if s.JTI != "" {
		fmt.Fprintf(w, "%-12s %s\n", "token jti:", s.JTI)
	}
	if s.Created != "" {
		fmt.Fprintf(w, "%-12s %s\n", "created:", s.Created)
	}
	return nil
}

// printBlastRadius prints the entities that would fall and the count of tokens
// that would be revoked by a removal, before the confirmation prompt.
func printBlastRadius(cmd *cobra.Command, fallen []store.EntityRecord, tokens int) {
	// The blast radius is prompt context, not command output: route it to stderr
	// so stdout stays a clean data channel.
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "Removing cascades to %d entit%s and revokes %d live token%s:\n",
		len(fallen), plural(len(fallen), "y", "ies"), tokens, plural(tokens, "", "s"))
	for _, e := range fallen {
		fmt.Fprintf(w, "  - %s (%s)\n", e.Path, e.Kind)
	}
}

// auditEntryJSON is the JSON shape of an audit-journal entry.
type auditEntryJSON struct {
	At     string `json:"at"`
	Op     string `json:"op"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// writeAudit reads and renders the audit journal under a path subtree.
func writeAudit(cmd *cobra.Command, st *store.Local, pathPrefix string) error {
	entries, err := st.Audit(pathPrefix)
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		out := make([]auditEntryJSON, len(entries))
		for i, e := range entries {
			out[i] = auditEntryJSON{At: e.At.UTC().Format(time.RFC3339), Op: string(e.Op), Path: e.Path, Detail: e.Detail}
		}
		return printJSON(cmd.OutOrStdout(), out)
	}
	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "no audit entries")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(w, "%s  %-16s %-24s %s\n", e.At.UTC().Format(time.RFC3339), e.Op, e.Path, e.Detail)
	}
	return nil
}

// plural picks the singular or plural suffix for n.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}
