package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// mintGrantFlags are the explicit extension-grant flags. Any of them, set
// on the command line, qualifies a mint under the fail-closed rule.
var mintGrantFlags = []string{"http", "grpc", "custom"}

// validateMintFlags enforces the fail-closed extension rule for token mint
// (ADR 0021): an unqualified mint is an error, never an unrestricted token.
// A mint must carry at least one of a --template, an explicit grant flag,
// or an explicit --no-extension. --no-extension is exclusive: it cannot be
// combined with a template or with grants, since those would add the very
// extensions it declines. A template and explicit grants may coexist; the
// grants union with the template's grants.
func validateMintFlags(hasTemplate, noExtension bool, grantCount int) error {
	qualified := hasTemplate || noExtension || grantCount > 0
	if !qualified {
		return errors.New("valiss: mint requires --template, at least one grant flag (--http/--grpc/--custom), or --no-extension")
	}
	if noExtension && (hasTemplate || grantCount > 0) {
		return errors.New("valiss: --no-extension cannot be combined with --template or grant flags")
	}
	return nil
}

// newTokenCommand builds the token noun. Tokens are issuances, not entities,
// and take the issuance verb set: mint, list, show, revoke (ADR 0021). A
// token is identified by its jti within an operator's store.
func newTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage tokens",
		Long:  "Mint, inspect, and revoke tokens, the dated issuances a valiss chain signs.",
	}

	mint := &cobra.Command{
		Use:   "mint <operator>/<account>/<user>",
		Short: "Mint a token for a user",
		Long: "Mint a token for the addressed user. The mint fails closed on " +
			"extensions: pass a --template, at least one grant flag, or an " +
			"explicit --no-extension.",
		Args: pathArgs(depthUser, depthUser, 0),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			template, err := cmd.Flags().GetString("template")
			if err != nil {
				return err
			}
			noExtension, err := cmd.Flags().GetBool("no-extension")
			if err != nil {
				return err
			}
			grantCount := 0
			for _, name := range mintGrantFlags {
				if cmd.Flags().Changed(name) {
					grantCount++
				}
			}
			return validateMintFlags(template != "", noExtension, grantCount)
		},
		RunE: stub,
	}
	mint.Flags().String("template", "", "claim template to stamp, as <name> (latest generation) or <name>@<gen>")
	mint.Flags().StringSlice("http", nil, "grant an HTTP extension for the given domain (repeatable)")
	mint.Flags().StringSlice("grpc", nil, "grant a gRPC extension for the given domain (repeatable)")
	mint.Flags().StringSlice("custom", nil, "grant a custom-scheme extension for the given domain (repeatable)")
	mint.Flags().Duration("ttl", 0, "token time-to-live, overriding any template TTL")
	mint.Flags().Bool("no-extension", false, "mint with no extensions (the explicit fail-closed opt-in)")
	mint.Flags().Bool("no-allowlist", false, "do not register the jti in the local allowlist")

	// list is scoped to the addressed entity subtree (operator, account, or
	// user), since there is no ambient context.
	list := &cobra.Command{
		Use:   "list <operator>[/<account>[/<user>]]",
		Short: "List tokens under an entity",
		Args:  pathArgs(depthOperator, depthUser, 0),
		RunE:  stub,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator> <jti>",
		Short: "Show a token",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  stub,
	}
	addJSONFlag(show)

	revoke := &cobra.Command{
		Use:   "revoke <operator> <jti>",
		Short: "Revoke a token",
		Long:  "Revoke a token: its jti leaves the operator's allowlist.",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  stub,
	}
	addYesFlag(revoke)

	cmd.AddCommand(mint, list, show, revoke)
	return cmd
}
