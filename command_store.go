package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	initCmd.Flags().Duration("audit-retention", defaultAuditRetention,
		"how long to retain audit-journal entries (0 keeps them forever)")

	info := &cobra.Command{
		Use:   "info <operator>",
		Short: "Show store facts",
		Long:  "Show read-only facts about an operator store (path, sizes, counts).",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(info)

	config := &cobra.Command{
		Use:   "config <operator> [<key> <value>]",
		Short: "Show or change store configuration",
		Long: "Show or change tunable store configuration. With no key, list " +
			"every tunable parameter and its current value; with a <key> " +
			"<value> pair, set one. Known keys: " + knownConfigKeys() + ".",
		Args: storeConfigArgs,
		RunE: stub,
	}
	addJSONFlag(config)

	cmd.AddCommand(initCmd, info, config)
	return cmd
}
