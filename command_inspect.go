package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"valiss.dev/valiss"
)

// newInspectCommand builds the inspect command: an offline decode of a
// token with no trust evaluation and no store access (ADR 0021).
func newInspectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <token>",
		Short: "Decode a token offline",
		Long: "Decode a token and print its claims. Offline only: no trust " +
			"evaluation, no store access. The self-signature carried in the " +
			"token is checked and reported, but the issuer's place in a trust " +
			"chain is not, so a token that decodes here is not thereby trusted.",
		Args: cobra.ExactArgs(1),
		RunE: runInspect,
	}
	addJSONFlag(cmd)
	return cmd
}

// tokenHeaderView is the decoded JWS header (typ/alg/ver).
type tokenHeaderView struct {
	Typ string `json:"typ"`
	Alg string `json:"alg"`
	Ver int    `json:"ver"`
}

// chainView mirrors a message token's embedded provenance chain.
type chainView struct {
	Account string `json:"account,omitempty"`
	User    string `json:"user,omitempty"`
}

// tokenView is the full offline view of a token: the header, the registered
// JWT claims, and the valiss body. It is the shape emitted by inspect --json.
//
// The valiss library's exported Decode surfaces only the registered claims
// (jti/iss/sub/iat/exp/nbf); to show the valiss body (type, epoch, bearer,
// extensions, message chain) and the header, inspect parses the JWS payload
// directly. This is sound precisely because inspect is an offline decode: it
// reads bytes and evaluates no trust.
type tokenView struct {
	Header tokenHeaderView `json:"header"`

	JTI       string `json:"jti,omitempty"`
	Issuer    string `json:"iss,omitempty"`
	Subject   string `json:"sub,omitempty"`
	Name      string `json:"name,omitempty"`
	Audience  string `json:"aud,omitempty"`
	IssuedAt  string `json:"iat,omitempty"`
	Expires   string `json:"exp,omitempty"`
	NotBefore string `json:"nbf,omitempty"`

	Type       string                     `json:"type,omitempty"`
	Epoch      uint64                     `json:"epoch,omitempty"`
	Bearer     bool                       `json:"bearer,omitempty"`
	Checksum   string                     `json:"checksum,omitempty"`
	Chain      *chainView                 `json:"chain,omitempty"`
	Extensions map[string]json.RawMessage `json:"ext,omitempty"`

	// Signature reports whether the token's own self-signature (the embedded
	// issuer key over the header and payload) verifies. It is not a trust
	// statement: a "valid" signature says only that the token was signed by
	// the key it names, not that the key is anchored anywhere.
	Signature string `json:"signature"`
}

// wirePayload is the JSON layout of a v1 token payload, matching the library's
// on-wire claims document. Only the fields inspect renders are named.
type wirePayload struct {
	JTI       string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	Issuer    string `json:"iss"`
	Name      string `json:"name"`
	Subject   string `json:"sub"`
	Audience  string `json:"aud"`
	Expires   int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
	Valiss    struct {
		Type     string                     `json:"type"`
		Epoch    uint64                     `json:"epoch"`
		Bearer   bool                       `json:"bearer"`
		Checksum string                     `json:"checksum"`
		Chain    *chainView                 `json:"chain"`
		Ext      map[string]json.RawMessage `json:"ext"`
	} `json:"valiss"`
}

func runInspect(cmd *cobra.Command, args []string) error {
	view, err := inspectToken(args[0])
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), view)
	}
	return view.writeText(cmd.OutOrStdout())
}

// inspectToken decodes a token's header and payload into a tokenView. It fails
// on a structurally malformed token (wrong segment count, bad base64, bad
// JSON); a well-formed token whose signature does not verify still decodes,
// with Signature set to "invalid".
func inspectToken(token string) (tokenView, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return tokenView{}, fmt.Errorf("valiss: malformed token: want 3 dot-separated segments, got %d", len(parts))
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return tokenView{}, fmt.Errorf("valiss: token header: %w", err)
	}
	var header tokenHeaderView
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return tokenView{}, fmt.Errorf("valiss: token header: %w", err)
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenView{}, fmt.Errorf("valiss: token claims: %w", err)
	}
	var p wirePayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return tokenView{}, fmt.Errorf("valiss: token claims: %w", err)
	}

	view := tokenView{
		Header:     header,
		JTI:        p.JTI,
		Issuer:     p.Issuer,
		Subject:    p.Subject,
		Name:       p.Name,
		Audience:   p.Audience,
		IssuedAt:   unixToRFC3339(p.IssuedAt),
		Expires:    unixToRFC3339(p.Expires),
		NotBefore:  unixToRFC3339(p.NotBefore),
		Type:       p.Valiss.Type,
		Epoch:      p.Valiss.Epoch,
		Bearer:     p.Valiss.Bearer,
		Checksum:   p.Valiss.Checksum,
		Chain:      p.Valiss.Chain,
		Extensions: p.Valiss.Ext,
		Signature:  "invalid",
	}
	// Decode re-checks the self-signature (issuer key embedded in the claims
	// over the signing input); a nil error means the token was signed by the
	// key it names. Trust is still the caller's to establish elsewhere.
	//
	// Judgement call (ADR 0021 does not specify inspect's checks): we verify
	// the self-signature but do not recompute and compare the jti. The docs
	// are explicit that the jti is a consistency check while the signature is
	// the integrity boundary, so a verified signature is the stronger
	// statement; recomputing the jti would also mean reproducing the library's
	// byte-exact claim serialization, which is fragile to duplicate here.
	if _, err := valiss.Decode(token); err == nil {
		view.Signature = "valid"
	}
	return view, nil
}

// unixToRFC3339 renders a Unix-seconds claim as UTC RFC3339, or "" for the
// zero (absent) value.
func unixToRFC3339(sec int64) string {
	if sec == 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// writeText renders a token view as an aligned key/value listing.
func (v tokenView) writeText(w io.Writer) error {
	line := func(k, val string) {
		if val != "" {
			fmt.Fprintf(w, "%-12s %s\n", k+":", val)
		}
	}
	line("type", v.Type)
	line("jti", v.JTI)
	line("name", v.Name)
	line("issuer", v.Issuer)
	line("subject", v.Subject)
	line("audience", v.Audience)
	if v.Epoch != 0 {
		line("epoch", fmt.Sprintf("%d", v.Epoch))
	}
	if v.Bearer {
		line("bearer", "true")
	}
	line("issued", v.IssuedAt)
	line("expires", v.Expires)
	line("notbefore", v.NotBefore)
	line("checksum", v.Checksum)
	if v.Chain != nil {
		line("chain", "account+user tokens embedded")
	}
	for name, body := range v.Extensions {
		line("ext:"+name, string(body))
	}
	line("signature", v.Signature)
	line("wire", fmt.Sprintf("%s/%s v%d", v.Header.Typ, v.Header.Alg, v.Header.Ver))
	return nil
}
