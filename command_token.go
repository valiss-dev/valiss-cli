package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
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
		Long: "Mint, list, show, and revoke tokens, the dated issuances a valiss " +
			"chain signs. To decode a token offline (no store, no trust " +
			"evaluation), use the top-level 'valiss inspect'.",
	}

	mint := &cobra.Command{
		Use:   "mint <operator>/<account>/<user>",
		Short: "Mint a token for a user",
		Long: "Mint a token for the addressed user. The mint fails closed on " +
			"extensions: pass a --template, at least one grant flag, or an " +
			"explicit --no-extension.",
		Example: "  # Stamp a claim template\n" +
			"  valiss token mint acme/team/alice --template web\n\n" +
			"  # A dimensioned HTTP grant (hosts, methods, paths)\n" +
			"  valiss token mint acme/team/alice --http \"hosts=api.example.com;methods=GET,POST;paths=/v1/*\"\n\n" +
			"  # A custom extension from a JSON file, plus a gRPC grant\n" +
			"  valiss token mint acme/team/alice --grpc /acme.v1.Widgets/Get --ext quota=@quota.json\n\n" +
			"  # Explicitly mint with no extensions (the fail-closed opt-in)\n" +
			"  valiss token mint acme/team/alice --no-extension\n\n" +
			"  # Capture the fresh token for a script\n" +
			"  valiss token mint acme/team/alice --no-extension --json",
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
	mint.Flags().Duration("ttl", 0, "token time-to-live, overriding any template TTL (0 = no expiry)")
	mint.Flags().Bool("bearer", false, "mint a bearer token, accepted without per-request signatures")
	mint.Flags().Bool("no-extension", false, "mint with no extensions (the explicit fail-closed opt-in)")
	addJSONFlag(mint)

	// list is scoped to the addressed entity subtree (operator, account, or
	// user), since there is no ambient context.
	list := &cobra.Command{
		Use:   "list <operator>[/<account>[/<user>]]",
		Short: "List tokens under an entity",
		Long:  "List the issuance records under the addressed entity's subtree (operator, account, or user): jti, level, status, and subject.",
		Args:  pathArgs(depthOperator, depthUser, 0),
		RunE:  runTokenList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator> <jti>",
		Short: "Show a token",
		Long:  "Show one issuance record by jti within an operator's store, including the token blob.",
		Args:  pathArgs(depthOperator, depthOperator, 1),
		RunE:  runTokenShow,
	}
	addJSONFlag(show)

	revoke := &cobra.Command{
		Use:   "revoke <operator> <jti>",
		Short: "Revoke a token",
		Long: "Revoke a token. Revocation a server enforces is an account jti " +
			"leaving the allowlist (verifier.go gates on the account jti), which " +
			"cryptographically cuts the account and every user beneath it. So a " +
			"revoke names an account jti. Per-user-token revocation is the " +
			"generation-floor mechanism of ADR 0022, not yet in valiss v0.13.1: " +
			"revoking a user jti is refused with guidance to revoke its account.",
		Args: pathArgs(depthOperator, depthOperator, 1),
		RunE: runTokenRevoke,
	}
	addYesFlag(revoke)

	cmd.AddCommand(mint, list, show, revoke)
	return cmd
}

// runTokenMint mints a user token for the addressed user and records the
// issuance. It does not touch the allowlist: a server verifies the allowlist
// against the ACCOUNT jti (verifier.go checks account.ID), not the user jti, so
// a user token is gated by its account's allowlist entry, deposited at
// 'account add'. Revoking the user's authority is revoking (or removing) its
// account.
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
	// A user token is gated by its account's allowlist entry, so report whether
	// that governing account jti is currently allowlisted: it tells a script
	// whether the fresh token will actually be admitted.
	allowlisted, err := accountAllowlisted(st, path)
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), mintResultJSON(rec, allowlisted))
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Minted token for %q\n  jti: %s\n", path, rec.JTI)
	if !rec.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "  expires: %s\n", rec.ExpiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(w, "  token: %s\n", rec.Token)
	return nil
}

// mintJSON is the machine-readable shape of a fresh mint (token mint --json):
// the jti, the subject, the token blob, its expiry, the bearer flag, and
// whether the governing account jti is allowlisted.
type mintJSON struct {
	JTI         string `json:"jti"`
	Subject     string `json:"subject"`
	Token       string `json:"token"`
	Expires     string `json:"expires,omitempty"`
	Bearer      bool   `json:"bearer"`
	Allowlisted bool   `json:"allowlisted"`
}

// mintResultJSON builds the JSON view of a fresh mint.
func mintResultJSON(rec store.TokenRecord, allowlisted bool) mintJSON {
	m := mintJSON{JTI: rec.JTI, Subject: rec.Subject, Token: rec.Token, Allowlisted: allowlisted}
	if !rec.ExpiresAt.IsZero() {
		m.Expires = rec.ExpiresAt.UTC().Format(time.RFC3339)
	}
	// Bearer lives in the valiss wire body, which the exported Decode does not
	// surface; the offline inspect decode reads it straight from the payload.
	if v, err := inspectToken(rec.Token); err == nil {
		m.Bearer = v.Bearer
	}
	return m
}

// accountAllowlisted reports whether the account governing a user path has its
// jti in the allowlist, which is what actually admits a user token minted under
// it (the allowlist keys on the account jti).
func accountAllowlisted(st *store.Local, userPath string) (bool, error) {
	acct, err := st.LiveEntity(parentOf(userPath))
	if err != nil {
		return false, err
	}
	claims, err := valiss.Decode(acct.Token)
	if err != nil {
		return false, fmt.Errorf("valiss: decoding account token: %w", err)
	}
	return st.AllowlistContains(claims.ID)
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
	if ttl < 0 {
		return mintParams{}, fmt.Errorf("valiss: --ttl must not be negative (got %s); use 0 for no expiry", ttl)
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

// runTokenRevoke revokes a token at the granularity a server actually enforces.
//
// The allowlist keys on account jtis, so an account jti leaving the allowlist
// is the only revocation valiss v0.13.1 enforces; it cuts the account and,
// cryptographically, every user beneath it (docs/concepts/allowlist.md,
// "Revoking an account kills its users"). A user jti is not on the allowlist
// and there is no per-user enforcement yet (ADR 0022 generation floors), so
// revoking one is refused with guidance rather than silently doing nothing.
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
	if rec.Level != store.KindAccount {
		return userRevokeUnsupported(st, rec)
	}
	if rec.Revoked {
		fmt.Fprintf(cmd.OutOrStdout(), "Token %q already revoked\n", jti)
		return nil
	}
	// The blast radius: the account plus its live users, cut in one edit.
	users, err := st.LiveJTIsUnder(rec.Subject)
	if err != nil {
		return err
	}
	ok, err := confirmed(cmd, fmt.Sprintf(
		"Revoke account jti %q? This removes it from the allowlist, cutting account %q and all its users (%d live descendant token(s)).",
		jti, rec.Subject, countUserTokens(users, jti)))
	if err != nil || !ok {
		return err
	}
	// Removing the account jti from the allowlist is the enforced revocation;
	// marking the descendant issuance records revoked keeps the store's view
	// consistent with the cryptographic reality that they no longer verify.
	if _, err := st.RevokeJTIsUnder(rec.Subject, time.Now().UTC()); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTokenRevoke, Path: rec.Subject,
		Detail: fmt.Sprintf("account jti=%s (cuts account and users)", jti)}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked account %q (jti %s left the allowlist; its users are cut)\n", rec.Subject, jti)
	return nil
}

// countUserTokens counts the live descendant tokens under an account that are
// not the account issuance itself.
func countUserTokens(under []string, accountJTI string) int {
	n := 0
	for _, j := range under {
		if j != accountJTI {
			n++
		}
	}
	return n
}

// userRevokeUnsupported reports that per-user-token revocation is not enforced
// by valiss v0.13.1 and points the operator at the account jti to revoke
// instead, naming it when the parent account resolves.
func userRevokeUnsupported(st *store.Local, rec store.TokenRecord) error {
	hint := "revoke its account (its jti is the allowlist key)"
	if acct, err := st.LiveEntity(parentOf(rec.Subject)); err == nil {
		if claims, derr := valiss.Decode(acct.Token); derr == nil {
			hint = fmt.Sprintf("revoke its account: token revoke %s %s (cuts the whole account and all its users)",
				operatorOf(rec.Subject), claims.ID)
		}
	}
	return fmt.Errorf("valiss: per-user-token revocation is not enforced by valiss v0.13.1 "+
		"(generation floors, ADR 0022, are not in the library yet); %s", hint)
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
