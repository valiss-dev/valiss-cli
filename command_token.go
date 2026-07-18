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
var mintGrantFlags = []string{"http", "grpc", "ext"}

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
		return errors.New("valiss: mint requires --template, at least one grant flag (--http/--grpc/--ext), or --no-extension")
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
	mint.Flags().StringArray("http", nil, "grant an HTTP extension (repeatable); a bare value is a host, or "+
		"\"hosts=a,b;methods=GET;paths=/v1/*\" to name dimensions")
	mint.Flags().StringArray("grpc", nil, "grant a gRPC extension for the given full method(s) (repeatable, comma-separated)")
	mint.Flags().StringArray("ext", nil, "grant a custom extension as <name>=<json> or <name>=@<file> (repeatable)")
	mint.Flags().Duration("ttl", 0, "token time-to-live, overriding any template TTL")
	mint.Flags().Bool("bearer", false, "mint a bearer token, accepted without per-request signatures")
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

// runTokenMint mints a token for the addressed user, records the issuance, and
// registers its jti in the local allowlist by default (ADR 0021).
func runTokenMint(cmd *cobra.Command, args []string) error {
	path := args[0]
	p, err := mintParamsFromFlags(cmd)
	if err != nil {
		return err
	}
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := mintToken(st, path, p)
	if err != nil {
		return err
	}
	noAllowlist, err := cmd.Flags().GetBool("no-allowlist")
	if err != nil {
		return err
	}
	registered := false
	if !noAllowlist {
		if registered, err = st.AddAllowlist(rec.JTI, time.Now().UTC()); err != nil {
			return err
		}
		if err := st.Append(store.AuditEntry{Op: store.AuditAllowlistAdd, Path: path,
			Detail: fmt.Sprintf("jti=%s (mint)", rec.JTI)}); err != nil {
			return err
		}
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Minted token for %q\n  jti: %s\n", path, rec.JTI)
	if !rec.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "  expires: %s\n", rec.ExpiresAt.UTC().Format(time.RFC3339))
	}
	switch {
	case noAllowlist:
		fmt.Fprintln(w, "  allowlist: skipped (--no-allowlist)")
	case registered:
		fmt.Fprintln(w, "  allowlist: registered")
	default:
		fmt.Fprintln(w, "  allowlist: already present")
	}
	fmt.Fprintf(w, "  token: %s\n", rec.Token)
	return nil
}

// mintParamsFromFlags reads the token mint grant and lifecycle flags.
func mintParamsFromFlags(cmd *cobra.Command) (mintParams, error) {
	template, err := cmd.Flags().GetString("template")
	if err != nil {
		return mintParams{}, err
	}
	http, err := cmd.Flags().GetStringArray("http")
	if err != nil {
		return mintParams{}, err
	}
	grpc, err := cmd.Flags().GetStringArray("grpc")
	if err != nil {
		return mintParams{}, err
	}
	ext, err := cmd.Flags().GetStringArray("ext")
	if err != nil {
		return mintParams{}, err
	}
	ttl, err := cmd.Flags().GetDuration("ttl")
	if err != nil {
		return mintParams{}, err
	}
	bearer, err := cmd.Flags().GetBool("bearer")
	if err != nil {
		return mintParams{}, err
	}
	noExtension, err := cmd.Flags().GetBool("no-extension")
	if err != nil {
		return mintParams{}, err
	}
	return mintParams{
		template:    template,
		http:        http,
		grpc:        grpc,
		ext:         ext,
		ttl:         ttl,
		ttlSet:      cmd.Flags().Changed("ttl"),
		bearer:      bearer,
		noExtension: noExtension,
	}, nil
}

// runTokenList lists the issuance records under the addressed entity's subtree.
func runTokenList(cmd *cobra.Command, args []string) error {
	path := args[0]
	st, err := openStore(operatorOf(path))
	if err != nil {
		return err
	}
	defer st.Close()

	recs, err := st.ListTokens(path)
	if err != nil {
		return err
	}
	summaries := make([]tokenSummary, len(recs))
	for i, r := range recs {
		summaries[i] = summarizeToken(r)
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
		fmt.Fprintf(w, "%s  %-7s %-7s %s\n", s.JTI, s.Level, s.Status, s.Subject)
	}
	return nil
}

// runTokenShow prints one issuance record, including the token blob.
func runTokenShow(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := st.Token(args[1])
	if errors.Is(err, store.ErrNoToken) {
		return fmt.Errorf("valiss: token %q not found", args[1])
	}
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	detail := tokenDetail{tokenSummary: summarizeToken(rec), Token: rec.Token}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), detail)
	}
	return writeTokenDetail(cmd, detail)
}

// runTokenRevoke revokes a token: it marks the record revoked and removes the
// jti from the allowlist.
func runTokenRevoke(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	jti := args[1]
	rec, err := st.Token(jti)
	if errors.Is(err, store.ErrNoToken) {
		return fmt.Errorf("valiss: token %q not found", jti)
	}
	if err != nil {
		return err
	}
	if rec.Revoked {
		fmt.Fprintf(cmd.OutOrStdout(), "Token %q already revoked\n", jti)
		return nil
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Revoke token %q (its jti leaves the allowlist)?", jti))
	if err != nil || !ok {
		return err
	}
	if err := st.RevokeToken(jti, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := st.RemoveAllowlist(jti); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTokenRevoke, Path: rec.Subject,
		Detail: fmt.Sprintf("jti=%s", jti)}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked token %q\n", jti)
	return nil
}

// tokenSummary is the display view of an issuance record.
type tokenSummary struct {
	JTI      string `json:"jti"`
	Subject  string `json:"subject"`
	Level    string `json:"level"`
	Status   string `json:"status"`
	Template string `json:"template,omitempty"`
	Minted   string `json:"minted,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Revoked  string `json:"revoked,omitempty"`
}

// tokenDetail adds the token blob to a summary, for token show.
type tokenDetail struct {
	tokenSummary
	Token string `json:"token"`
}

// summarizeToken builds a display summary from an issuance record.
func summarizeToken(r store.TokenRecord) tokenSummary {
	s := tokenSummary{JTI: r.JTI, Subject: r.Subject, Level: r.Level, Status: tokenStatus(r)}
	if r.TemplateName != "" {
		s.Template = fmt.Sprintf("%s@%d", r.TemplateName, r.TemplateGen)
	}
	if !r.MintedAt.IsZero() {
		s.Minted = r.MintedAt.UTC().Format(time.RFC3339)
	}
	if !r.ExpiresAt.IsZero() {
		s.Expires = r.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if r.Revoked && !r.RevokedAt.IsZero() {
		s.Revoked = r.RevokedAt.UTC().Format(time.RFC3339)
	}
	return s
}

// tokenStatus reports a token's lifecycle status: revoked, expired, or live.
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

// writeTokenDetail renders one issuance record as text.
func writeTokenDetail(cmd *cobra.Command, d tokenDetail) error {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-12s %s\n", "jti:", d.JTI)
	fmt.Fprintf(w, "%-12s %s\n", "subject:", d.Subject)
	fmt.Fprintf(w, "%-12s %s\n", "level:", d.Level)
	fmt.Fprintf(w, "%-12s %s\n", "status:", d.Status)
	if d.Template != "" {
		fmt.Fprintf(w, "%-12s %s\n", "template:", d.Template)
	}
	if d.Minted != "" {
		fmt.Fprintf(w, "%-12s %s\n", "minted:", d.Minted)
	}
	if d.Expires != "" {
		fmt.Fprintf(w, "%-12s %s\n", "expires:", d.Expires)
	}
	if d.Revoked != "" {
		fmt.Fprintf(w, "%-12s %s\n", "revoked:", d.Revoked)
	}
	fmt.Fprintf(w, "%-12s %s\n", "token:", d.Token)
	return nil
}
