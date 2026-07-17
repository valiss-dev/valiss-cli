package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
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
		RunE: runTokenMint,
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
		RunE:  runTokenList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator> <jti>",
		Short: "Show a token",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  runTokenShow,
	}
	addJSONFlag(show)

	revoke := &cobra.Command{
		Use:   "revoke <operator> <jti>",
		Short: "Revoke a token",
		Long:  "Revoke a token: its jti leaves the operator's allowlist.",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  runTokenRevoke,
	}
	addYesFlag(revoke)

	cmd.AddCommand(mint, list, show, revoke)
	return cmd
}

// tokenSummary is the display view of a minted issuance.
type tokenSummary struct {
	JTI      string `json:"jti"`
	Subject  string `json:"subject"`
	Level    string `json:"level"`
	Template string `json:"template,omitempty"`
	Minted   string `json:"minted,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Revoked  bool   `json:"revoked"`
	Status   string `json:"status"`
	Token    string `json:"token,omitempty"`
}

// summarizeToken builds a display summary from a persisted token. withToken
// includes the token string (show, not list).
func summarizeToken(r store.TokenRecord, withToken bool) tokenSummary {
	s := tokenSummary{
		JTI:     r.JTI,
		Subject: r.Subject,
		Level:   r.Level,
		Revoked: r.Revoked,
		Status:  tokenStatus(r),
	}
	if r.TemplateName != "" {
		s.Template = fmt.Sprintf("%s@%d", r.TemplateName, r.TemplateGen)
	}
	if !r.MintedAt.IsZero() {
		s.Minted = r.MintedAt.UTC().Format(time.RFC3339)
	}
	if !r.ExpiresAt.IsZero() {
		s.Expires = r.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if withToken {
		s.Token = r.Token
	}
	return s
}

// tokenStatus classifies a token as revoked, expired, or live.
func tokenStatus(r store.TokenRecord) string {
	switch {
	case r.Revoked:
		return "revoked"
	case !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt):
		return "expired"
	default:
		return "live"
	}
}

func runTokenMint(cmd *cobra.Command, args []string) error {
	req, err := mintRequestFromFlags(cmd, args[0])
	if err != nil {
		return err
	}
	st, err := openStore(operatorOf(args[0]))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := mintToken(st, req)
	if err != nil {
		return err
	}
	// The token goes to stdout alone, so `valiss token mint ... > tok.jwt`
	// captures exactly the credential; the human-readable metadata goes to
	// stderr (this mirrors the library's minter and keeps mint scriptable
	// without a --json flag, which ADR 0021 reserves for list and show).
	fmt.Fprintln(cmd.OutOrStdout(), rec.Token)
	meta := cmd.ErrOrStderr()
	fmt.Fprintf(meta, "minted token for %q\n  jti: %s\n", rec.Subject, rec.JTI)
	if rec.TemplateName != "" {
		fmt.Fprintf(meta, "  template: %s@%d\n", rec.TemplateName, rec.TemplateGen)
	}
	if !rec.ExpiresAt.IsZero() {
		fmt.Fprintf(meta, "  expires: %s\n", rec.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if !req.noAllowlist {
		fmt.Fprintln(meta, "  allowlisted: yes")
	}
	return nil
}

func runTokenList(cmd *cobra.Command, args []string) error {
	st, err := openStore(operatorOf(args[0]))
	if err != nil {
		return err
	}
	defer st.Close()

	recs, err := st.ListTokensUnder(args[0])
	if err != nil {
		return err
	}
	summaries := make([]tokenSummary, 0, len(recs))
	for _, r := range recs {
		summaries = append(summaries, summarizeToken(r, false))
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
		fmt.Fprintln(w, "no tokens")
		return nil
	}
	for _, s := range summaries {
		fmt.Fprintf(w, "%s  %-24s %-8s %s\n", s.JTI, s.Subject, s.Status, s.Template)
	}
	return nil
}

func runTokenShow(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.GetToken(args[1])
	if errors.Is(err, store.ErrNoToken) {
		return err
	}
	if err != nil {
		return err
	}
	s := summarizeToken(rec, true)
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), s)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-10s %s\n", "jti:", s.JTI)
	fmt.Fprintf(w, "%-10s %s\n", "subject:", s.Subject)
	fmt.Fprintf(w, "%-10s %s\n", "status:", s.Status)
	if s.Template != "" {
		fmt.Fprintf(w, "%-10s %s\n", "template:", s.Template)
	}
	if s.Minted != "" {
		fmt.Fprintf(w, "%-10s %s\n", "minted:", s.Minted)
	}
	if s.Expires != "" {
		fmt.Fprintf(w, "%-10s %s\n", "expires:", s.Expires)
	}
	fmt.Fprintf(w, "\n%s\n", s.Token)
	return nil
}

func runTokenRevoke(cmd *cobra.Command, args []string) error {
	operator, jti := args[0], args[1]
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	if _, err := st.GetToken(jti); err != nil {
		return err
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Revoke token %s (remove its jti from the allowlist)?", jti))
	if err != nil || !ok {
		return err
	}
	if err := st.RevokeToken(jti, time.Now().UTC()); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTokenRevoke, Path: operator, Detail: jti}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked token %s\n", jti)
	return nil
}

// mintRequestFromFlags reads the mint flags into a mintRequest.
func mintRequestFromFlags(cmd *cobra.Command, userPath string) (mintRequest, error) {
	req := mintRequest{userPath: userPath}
	templateStr, err := cmd.Flags().GetString("template")
	if err != nil {
		return req, err
	}
	if templateStr != "" {
		// The template is addressed within the mint's operator: build an
		// operator-qualified reference so the existing parser applies.
		ref, err := parseTemplateRef(operatorOf(userPath) + "/" + templateStr)
		if err != nil {
			return req, err
		}
		req.template = &ref
	}
	if req.http, err = cmd.Flags().GetStringSlice("http"); err != nil {
		return req, err
	}
	if req.grpc, err = cmd.Flags().GetStringSlice("grpc"); err != nil {
		return req, err
	}
	if req.custom, err = cmd.Flags().GetStringSlice("custom"); err != nil {
		return req, err
	}
	if req.ttl, err = cmd.Flags().GetDuration("ttl"); err != nil {
		return req, err
	}
	req.ttlSet = cmd.Flags().Changed("ttl")
	if req.noExtension, err = cmd.Flags().GetBool("no-extension"); err != nil {
		return req, err
	}
	if req.noAllowlist, err = cmd.Flags().GetBool("no-allowlist"); err != nil {
		return req, err
	}
	return req, nil
}
