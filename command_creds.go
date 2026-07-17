package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss/creds"
)

// newCredsCommand builds the creds noun: export of the creds artifacts the
// format supports (ADR 0021). The path addresses the entity whose creds are
// exported.
func newCredsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "creds",
		Short: "Export credential artifacts",
	}

	export := &cobra.Command{
		Use:   "export <operator>[/<account>[/<user>]]",
		Short: "Export creds for an entity",
		Long: "Export creds for the addressed entity to stdout, covering the " +
			"creds kinds the format supports: account-level (account token + " +
			"account seed), user-level (user token + user seed), a bundle " +
			"(--bundle, which adds the account token), and bearer (--bearer, " +
			"tokens only, no seed). The creds format has no operator-level form.",
		Args: pathArgs(depthOperator, depthUser, 0),
		RunE: runCredsExport,
	}
	export.Flags().Bool("bundle", false, "embed the account token in user creds (for servers that do not resolve it)")
	export.Flags().Bool("bearer", false, "export tokens only, without the signing seed")

	cmd.AddCommand(export)
	return cmd
}

func runCredsExport(cmd *cobra.Command, args []string) error {
	path := args[0]
	bundle, err := cmd.Flags().GetBool("bundle")
	if err != nil {
		return err
	}
	bearer, err := cmd.Flags().GetBool("bearer")
	if err != nil {
		return err
	}

	depth := strings.Count(path, "/") + 1
	if depth == depthOperator {
		// The creds file format (creds.Creds) carries account and user tokens
		// only; there is no operator-token marker. ADR 0021's path range spans
		// the operator for uniformity, but an operator has no creds-file form —
		// its seed is handled through custody/backup, not a creds export.
		return fmt.Errorf("valiss: the creds format has no operator-level form; export account (<operator>/<account>) or user (<operator>/<account>/<user>) creds")
	}
	if depth == depthAccount && bundle {
		return fmt.Errorf("valiss: --bundle applies only to user creds")
	}

	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	var (
		out  creds.Creds
		kind string
	)
	switch depth {
	case depthAccount:
		acct, err := st.LiveEntity(path)
		if err != nil {
			return err
		}
		out.AccountToken = acct.Token
		if !bearer {
			out.Seed = acct.Seed
		}
		kind = "account"
	case depthUser:
		user, err := st.LiveEntity(path)
		if err != nil {
			return err
		}
		out.UserToken = user.Token
		if !bearer {
			out.Seed = user.Seed
		}
		if bundle {
			acct, err := st.LiveEntity(parentOf(path))
			if err != nil {
				return err
			}
			out.AccountToken = acct.Token
		}
		if bearer {
			warnIfNotBearer(cmd, user.Token)
		}
		kind = "user"
	}

	fmt.Fprint(cmd.OutOrStdout(), creds.Format(out))
	if err := st.Append(store.AuditEntry{Op: store.AuditCredsExport, Path: path,
		Detail: credsKindDetail(kind, bundle, bearer)}); err != nil {
		return err
	}
	return nil
}

// warnIfNotBearer notes on stderr when a bearer export carries a token that was
// not minted as a bearer token, since a server accepts bearer creds only when
// the effective token is a bearer user token.
func warnIfNotBearer(cmd *cobra.Command, token string) {
	view, err := inspectToken(token)
	if err != nil || view.Bearer {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(),
		"warning: --bearer omits the seed, but this token was not minted as a bearer token; "+
			"a server will reject it. Mint a bearer token (a bearer template) for bearer creds.")
}

// credsKindDetail describes an export for the audit journal.
func credsKindDetail(kind string, bundle, bearer bool) string {
	detail := kind
	if bundle {
		detail += " bundle"
	}
	if bearer {
		detail += " bearer"
	}
	return detail
}
