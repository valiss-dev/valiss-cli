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
	}

	export := &cobra.Command{
		Use:   "export <operator>[/<account>[/<user>]]",
		Short: "Export creds for an entity",
		Long: "Export creds for the addressed entity, covering the four creds " +
			"kinds the format supports: account, user, bundle (--bundle, a user " +
			"creds that also embeds the account token), and bearer (--bearer, " +
			"tokens only, no seed).",
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

// assembleCreds builds the creds file content for the addressed entity. It
// covers the four creds kinds: account creds (the account token and account
// seed), user creds (the user token and user seed), a bundle (user creds that
// also embed the account token), and bearer creds (tokens only, no seed).
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
		acct, err := st.LiveEntity(path)
		if errors.Is(err, store.ErrNoEntity) {
			return creds.Creds{}, "", fmt.Errorf("valiss: account %q not found", path)
		} else if err != nil {
			return creds.Creds{}, "", err
		}
		b.AccountToken = acct.Token
		if !bearer {
			b.Seed = acct.Seed
		}
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
		if !bearer {
			b.Seed = user.Seed
		}
		kind = "user"
		if bundle {
			kind = "bundle"
		}
	default:
		return creds.Creds{}, "", errors.New("valiss: creds are account- or user-level; an operator has no creds file")
	}
	if bearer {
		kind = "bearer"
	}
	return b, kind, nil
}
