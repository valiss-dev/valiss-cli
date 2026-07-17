package main

import (
	"time"

	"github.com/spf13/cobra"
)

// defaultAuditRetention is the store-global audit retention default: 2160h,
// ninety days (ADR 0021). Zero keeps the journal forever.
const defaultAuditRetention = 2160 * time.Hour

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
		Short: "Show store information",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(info)

	config := &cobra.Command{
		Use:   "config <operator>",
		Short: "Show store configuration",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(config)

	cmd.AddCommand(initCmd, info, config)
	return cmd
}
