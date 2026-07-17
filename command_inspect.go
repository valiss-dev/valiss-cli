package main

import "github.com/spf13/cobra"

// newInspectCommand builds the inspect command: an offline decode of a
// token with no trust evaluation and no store access (ADR 0021).
func newInspectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <token>",
		Short: "Decode a token offline",
		Long:  "Decode a token and print its claims. Offline only: no trust evaluation, no store access.",
		Args:  cobra.ExactArgs(1),
		RunE:  stub,
	}
	addJSONFlag(cmd)
	return cmd
}
