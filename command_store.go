package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

// defaultAuditRetention is the store-global audit retention default: 2160h,
// ninety days (ADR 0021). Zero keeps the journal forever.
const defaultAuditRetention = 2160 * time.Hour

// storeConfigKeys are the tunable store parameters, each with a validator
// for its value. Audit retention is adjustable after init (ADR 0021), and
// store config is its designated mutator. The map is the single source of
// truth for which keys store config accepts.
var storeConfigKeys = map[string]func(string) error{
	"audit-retention": func(v string) error {
		if _, err := time.ParseDuration(v); err != nil {
			return fmt.Errorf("audit-retention must be a duration: %w", err)
		}
		return nil
	},
}

// knownConfigKeys renders the settable keys in sorted order for help and
// error text.
func knownConfigKeys() string {
	keys := make([]string, 0, len(storeConfigKeys))
	for k := range storeConfigKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// storeConfigArgs validates the git-style get/set argument shape: an
// operator path, then either nothing (list all parameters) or exactly a
// <key> <value> pair (set one). A lone key with no value is rejected. The
// key is checked against the known set and the value against the key's
// validator, so unknown keys and unparseable values fail before any body
// runs.
func storeConfigArgs(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 1, 3:
	default:
		return fmt.Errorf("accepts an operator and an optional <key> <value> pair, received %d arg(s)", len(args))
	}
	if err := validatePath(args[0], depthOperator, depthOperator); err != nil {
		return err
	}
	if len(args) == 1 {
		return nil
	}
	key, value := args[1], args[2]
	validate, ok := storeConfigKeys[key]
	if !ok {
		return fmt.Errorf("unknown config key %q (known: %s)", key, knownConfigKeys())
	}
	return validate(value)
}

func newStoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Manage the credential store",
		Long:  "Initialize and inspect an operator's encrypted store (ADR 0020).",
	}

	initCmd := &cobra.Command{
		Use:   "init <operator>",
		Short: "Initialize an operator store",
		Long: "Initialize an encrypted store for an operator (ADR 0020). The " +
			"store is a configured, empty container: the operator identity is " +
			"created later with 'operator add'.",
		Args: pathArgs(depthOperator, depthOperator, 0),
		RunE: runStoreInit,
	}
	initCmd.Flags().Duration("audit-retention", defaultAuditRetention,
		"how long to retain audit-journal entries (0 keeps them forever)")

	info := &cobra.Command{
		Use:   "info <operator>",
		Short: "Show store facts",
		Long:  "Show read-only facts about an operator store (path, sizes, counts).",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runStoreInfo,
	}
	addJSONFlag(info)

	config := &cobra.Command{
		Use:   "config <operator> [<key> <value>]",
		Short: "Show or change store configuration",
		Long: "Show or change tunable store configuration. With no key, list " +
			"every tunable parameter and its current value; with a <key> " +
			"<value> pair, set one. Known keys: " + knownConfigKeys() + ".",
		Args: storeConfigArgs,
		RunE: runStoreConfig,
	}
	addJSONFlag(config)

	cmd.AddCommand(initCmd, info, config)
	return cmd
}

// runStoreInit initializes an operator store with the requested audit
// retention, prompting for (and confirming) the storage passphrase when
// VALISS_STORAGE_KEY is unset.
func runStoreInit(cmd *cobra.Command, args []string) error {
	operator := args[0]
	retention, err := cmd.Flags().GetDuration("audit-retention")
	if err != nil {
		return err
	}
	st, err := initStore(operator, store.Config{AuditRetention: retention})
	if err != nil {
		return err
	}
	defer st.Close()
	info, err := st.Info()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Initialized store for %q at %s\n", operator, info.Path)
	return nil
}

// runStoreInfo prints read-only store facts, as text or JSON.
func runStoreInfo(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()
	info, err := st.Info()
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), storeInfoJSON(info))
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-16s %s\n", "operator:", info.Operator)
	fmt.Fprintf(w, "%-16s %s\n", "path:", info.Path)
	fmt.Fprintf(w, "%-16s %s\n", "created:", info.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "%-16s spec %d / wire %d\n", "format:", info.SpecVersion, info.WireVersion)
	fmt.Fprintf(w, "%-16s %s\n", "audit-retention:", retentionString(info.AuditRetention))
	fmt.Fprintf(w, "%-16s %d\n", "entities:", info.Entities)
	fmt.Fprintf(w, "%-16s %d\n", "tokens:", info.Tokens)
	fmt.Fprintf(w, "%-16s %d\n", "templates:", info.Templates)
	fmt.Fprintf(w, "%-16s %d\n", "allowlist:", info.Allowlist)
	fmt.Fprintf(w, "%-16s %d\n", "audit lines:", info.AuditLines)
	fmt.Fprintf(w, "%-16s %d\n", "size (bytes):", info.SizeBytes)
	return nil
}

// runStoreConfig lists the tunable parameters, or sets one. The argument shape
// is validated by storeConfigArgs before this runs.
func runStoreConfig(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	if len(args) == 3 {
		if err := st.SetConfig(args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", args[1], args[2])
		return nil
	}

	cfg, err := st.Config()
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), map[string]string{
			"audit-retention": retentionString(cfg.AuditRetention),
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%-16s %s\n", "audit-retention:", retentionString(cfg.AuditRetention))
	return nil
}

// storeInfoJSON is the JSON shape of store info: durations rendered as strings
// so the output stays human-legible while machine-parseable.
func storeInfoJSON(info store.Info) map[string]any {
	return map[string]any{
		"operator":        info.Operator,
		"path":            info.Path,
		"created":         info.CreatedAt.Format(time.RFC3339),
		"spec_version":    info.SpecVersion,
		"wire_version":    info.WireVersion,
		"audit_retention": retentionString(info.AuditRetention),
		"entities":        info.Entities,
		"tokens":          info.Tokens,
		"templates":       info.Templates,
		"allowlist":       info.Allowlist,
		"audit_lines":     info.AuditLines,
		"size_bytes":      info.SizeBytes,
	}
}

// retentionString renders a retention window, spelling out the keep-forever
// case (zero) rather than printing "0s".
func retentionString(d time.Duration) string {
	if d <= 0 {
		return "forever"
	}
	return d.String()
}
