package main

import (
	"errors"
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
		Long: "Export credential artifacts for an entity. Creds are written to " +
			"stdout in the valiss creds format; redirect to a file to save them. " +
			"Export serves the entity's current token (its latest generation, as " +
			"re-issued by 'token mint'). A creds file carries a signing seed " +
			"unless --bearer is given, so treat it as a secret.",
	}

	export := &cobra.Command{
		Use:   "export <operator>[/<account>[/<user>]]",
		Short: "Export creds for an entity",
		Long: "Export creds for the addressed entity's current token, covering the " +
			"four creds kinds the format supports: account, user, bundle " +
			"(--bundle, a user creds that also embeds the account token), and " +
			"bearer (--bearer, tokens only, no seed). --bearer serves the current " +
			"token as bearer creds and is refused unless that token was minted as " +
			"a bearer token ('token mint --bearer'), so it can never emit creds " +
			"that fail to authenticate. Output goes to stdout; redirect it to a file.",
		Example: "  # Account creds (account token + seed)\n" +
			"  valiss creds export acme/team > team.creds\n\n" +
			"  # User creds\n" +
			"  valiss creds export acme/team/alice > alice.creds\n\n" +
			"  # A bundle: user creds that also embed the account token\n" +
			"  valiss creds export acme/team/alice --bundle > alice.creds\n\n" +
			"  # Bearer creds: tokens only, no signing seed\n" +
			"  valiss creds export acme/team/alice --bearer > alice.bearer.creds",
		Args: pathArgs(depthOperator, depthUser, 0),
		RunE: runCredsExport,
	}
	export.Flags().Bool("bundle", false, "user creds that also embed the account token")
	export.Flags().Bool("bearer", false, "export tokens only, without the signing seed")

	cmd.AddCommand(export)
	return cmd
}

// runCredsExport assembles and writes a creds file for the addressed entity.
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
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	b, kind, err := assembleCreds(st, path, bundle, bearer)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), creds.Format(b))
	return st.Append(store.AuditEntry{Op: store.AuditCredsExport, Path: path,
		Detail: fmt.Sprintf("kind=%s bundle=%t bearer=%t", kind, bundle, bearer)})
}

// assembleCreds builds the creds file content for the addressed entity's
// CURRENT token (its latest generation), plus the signing seed. It covers the
// four creds kinds: account creds (the account token and account seed), user
// creds (the current user token and user seed), a bundle (user creds that also
// embed the account token), and bearer creds (tokens only, no seed).
//
// The --bearer reconciliation (the model change): a token is a bearer token
// because it was minted as one (token mint --bearer), not because export says
// so. So --bearer on export means "serve the current token as bearer creds",
// and it is refused unless the current token actually is a bearer token. This
// closes the old footgun where --bearer stripped the seed off a non-bearer
// token and produced creds that could never authenticate: bearer creds carry
// no seed, and the verifier waives the request signature only for a bearer
// user token. Account creds can never be bearer (account requests must always
// sign; only user tokens carry the bearer flag), so --bearer on an account is
// refused outright. Default export (no --bearer) always carries the seed, so
// its creds authenticate whatever the current token is.
func assembleCreds(st *store.Local, path string, bundle, bearer bool) (creds.Creds, string, error) {
	depth := strings.Count(path, "/") + 1
	var (
		b    creds.Creds
		kind string
	)
	switch depth {
	case depthAccount:
		if bundle {
			return creds.Creds{}, "", errors.New("valiss: --bundle applies to user creds; an account's creds already carry the account token")
		}
		if bearer {
			return creds.Creds{}, "", errors.New("valiss: account creds cannot be bearer: account requests must be signed (only a user token, minted with 'token mint --bearer', can be a bearer token)")
		}
		acct, err := st.LiveEntity(path)
		if errors.Is(err, store.ErrNoEntity) {
			return creds.Creds{}, "", fmt.Errorf("valiss: account %q not found", path)
		} else if err != nil {
			return creds.Creds{}, "", err
		}
		b.AccountToken = acct.Token
		b.Seed = acct.Seed
		kind = "account"
	case depthUser:
		user, err := st.LiveEntity(path)
		if errors.Is(err, store.ErrNoEntity) {
			return creds.Creds{}, "", fmt.Errorf("valiss: user %q not found", path)
		} else if err != nil {
			return creds.Creds{}, "", err
		}
		b.UserToken = user.Token
		if bundle {
			acct, err := st.LiveEntity(parentOf(path))
			if err != nil {
				return creds.Creds{}, "", err
			}
			b.AccountToken = acct.Token
		}
		if bearer {
			isBearer, err := bearerToken(user.Token)
			if err != nil {
				return creds.Creds{}, "", err
			}
			if !isBearer {
				return creds.Creds{}, "", fmt.Errorf(
					"valiss: current token for %q is not a bearer token; re-issue it with 'token mint %s --bearer ...' before exporting bearer creds "+
						"(bearer creds carry no seed, so a non-bearer token in them could never authenticate)", path, path)
			}
			kind = "bearer"
		} else {
			b.Seed = user.Seed
			kind = "user"
			if bundle {
				kind = "bundle"
			}
		}
	default:
		return creds.Creds{}, "", errors.New("valiss: creds are account- or user-level; an operator has no creds file")
	}
	return b, kind, nil
}

// bearerToken reports whether a token carries the bearer flag. It reads the
// flag from the token body via the offline decode (the exported Decode does
// not surface the valiss body); this is sound because it only inspects bytes
// the store itself minted.
func bearerToken(token string) (bool, error) {
	v, err := inspectToken(token)
	if err != nil {
		return false, fmt.Errorf("valiss: inspecting current token: %w", err)
	}
	return v.Bearer, nil
}
