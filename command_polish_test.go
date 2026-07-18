package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// runCLIIO executes the root command with args, feeding stdin and capturing
// stdout and stderr separately, so tests can assert stream routing and exit
// behavior (a non-nil error stands in for a non-zero exit).
func runCLIIO(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	viper.Reset()
	root := newRootCommand()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

// TestOperatorListEmptyJSON guards that an empty operator list emits [] on
// --json, matching every other list, rather than null.
func TestOperatorListEmptyJSON(t *testing.T) {
	operatorEnv(t)
	out, err := runCLI(t, "operator", "list", "--json")
	if err != nil {
		t.Fatalf("operator list --json: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty operator list --json = %q, want []", strings.TrimSpace(out))
	}
}

// TestMintJSON asserts token mint --json emits the documented object shape.
func TestMintJSON(t *testing.T) {
	tokenEnv(t)
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com", "--json")
	if err != nil {
		t.Fatalf("token mint --json: %v\n%s", err, out)
	}
	var m struct {
		JTI         string `json:"jti"`
		Subject     string `json:"subject"`
		Token       string `json:"token"`
		Bearer      bool   `json:"bearer"`
		Allowlisted bool   `json:"allowlisted"`
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("decode mint json: %v\n%s", err, out)
	}
	if m.JTI == "" || m.Token == "" {
		t.Errorf("mint json missing jti/token: %+v", m)
	}
	if m.Subject != "acme/team/alice" {
		t.Errorf("mint json subject = %q, want acme/team/alice", m.Subject)
	}
	// The governing account jti was deposited in the allowlist at account add.
	if !m.Allowlisted {
		t.Errorf("mint json allowlisted = false, want true (account jti is allowlisted)")
	}
}

// TestMintEmptyGrantRejected asserts a grant-only mint whose grant values are
// all blank is refused rather than minting an extension-less token.
func TestMintEmptyGrantRejected(t *testing.T) {
	for _, arg := range []string{"", ",,"} {
		tokenEnv(t)
		if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", arg); err == nil {
			t.Errorf("mint --http %q succeeded; want empty-grant rejection", arg)
		}
	}
	tokenEnv(t)
	if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--grpc", " "); err == nil {
		t.Error("mint --grpc ' ' succeeded; want empty-grant rejection")
	}
}

// TestMintNegativeTTLRejected asserts a negative --ttl is refused (a silently
// dropped negative TTL would mint a never-expiring token).
func TestMintNegativeTTLRejected(t *testing.T) {
	tokenEnv(t)
	if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--no-extension", "--ttl", "-1h"); err == nil {
		t.Error("mint --ttl -1h succeeded; want negative-ttl rejection")
	}
	// Zero is explicitly allowed (no expiry).
	if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--no-extension", "--ttl", "0"); err != nil {
		t.Errorf("mint --ttl 0 failed: %v", err)
	}
}

// TestTemplateNegativeTTLRejected asserts template add also refuses a negative
// --ttl.
func TestTemplateNegativeTTLRejected(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "template", "add", "acme/web", "--http", "x", "--ttl", "-1h"); err == nil {
		t.Error("template add --ttl -1h succeeded; want negative-ttl rejection")
	}
}

// TestStoreInitNegativeRetentionRejected asserts store init validates the
// retention window like store config does.
func TestStoreInitNegativeRetentionRejected(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "store", "init", "acme", "--audit-retention", "-1h"); err == nil {
		t.Error("store init --audit-retention -1h succeeded; want rejection")
	}
}

// TestConfirmationDeclineExitsNonZero asserts a declined destructive
// confirmation returns an error (non-zero exit) and routes its prompt and
// blast-radius listing to stderr, keeping stdout clean.
func TestConfirmationDeclineExitsNonZero(t *testing.T) {
	for _, stdin := range []string{"n\n", ""} { // explicit no, and EOF
		tokenEnv(t)
		stdout, stderr, err := runCLIIO(t, stdin, "operator", "remove", "acme")
		if err == nil {
			t.Errorf("declined remove (stdin %q) returned nil error; want non-zero exit", stdin)
		}
		if strings.Contains(stdout, "Removing cascades") || strings.Contains(stdout, "[y/N]") {
			t.Errorf("prompt/blast-radius leaked to stdout (stdin %q):\n%s", stdin, stdout)
		}
		if !strings.Contains(stderr, "Removing cascades") || !strings.Contains(stderr, "[y/N]") {
			t.Errorf("prompt/blast-radius not on stderr (stdin %q):\n%s", stdin, stderr)
		}
		// The operator must survive a declined removal.
		if _, err := runCLI(t, "operator", "show", "acme"); err != nil {
			t.Errorf("operator gone after a declined remove (stdin %q): %v", stdin, err)
		}
	}
}

// TestMissingOperatorErrorClean asserts a command against a nonexistent
// operator reports a clean operator-not-found error without leaking the raw
// .db path.
func TestMissingOperatorErrorClean(t *testing.T) {
	operatorEnv(t)
	_, err := runCLI(t, "operator", "show", "ghost")
	if err == nil {
		t.Fatal("operator show of a missing operator succeeded; want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `operator "ghost" not found`) {
		t.Errorf("missing-operator error = %q, want it to name the operator", msg)
	}
	if strings.Contains(msg, ".db") {
		t.Errorf("missing-operator error leaks the .db path: %q", msg)
	}
}

// TestErrorPrefixNormalized asserts a bare validation/arity error is rendered
// with the house valiss: prefix by the single error sink.
func TestErrorPrefixNormalized(t *testing.T) {
	cases := []struct{ in, want string }{
		{"path \"x/y\" must name an operator", "valiss: path \"x/y\" must name an operator"},
		{"valiss: already prefixed", "valiss: already prefixed"},
	}
	for _, c := range cases {
		if got := normalizeError(errString(c.in)); got != c.want {
			t.Errorf("normalizeError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// errString is a trivial error carrying a fixed message.
type errString string

func (e errString) Error() string { return string(e) }

// TestMintGrantlessTemplateRejected asserts a template that carries no
// extensions cannot mint a zero-extension token without the explicit
// --no-extension opt-in: the grantless-template bypass is closed.
func TestMintGrantlessTemplateRejected(t *testing.T) {
	tokenEnv(t)
	// A template with only a TTL carries no extensions.
	if out, err := runCLI(t, "template", "add", "acme/bare", "--ttl", "1h"); err != nil {
		t.Fatalf("template add: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--template", "bare"); err == nil {
		t.Error("grantless-template mint succeeded; want zero-extension rejection")
	}
	// The same mint with --no-extension is the legitimate opt-in and is accepted.
	if out, err := runCLI(t, "token", "mint", "acme/team/alice", "--template", "bare", "--no-extension"); err != nil {
		t.Errorf("grantless-template mint with --no-extension failed: %v\n%s", err, out)
	}
}

// TestMintNoExtensionContradictsGrantedTemplate asserts --no-extension against a
// template that does carry extensions is refused as a contradiction, rather
// than silently dropping the template's grants.
func TestMintNoExtensionContradictsGrantedTemplate(t *testing.T) {
	tokenEnv(t)
	if out, err := runCLI(t, "template", "add", "acme/web", "--http", "api.example.com"); err != nil {
		t.Fatalf("template add: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--template", "web", "--no-extension"); err == nil {
		t.Error("--no-extension against a granted template succeeded; want contradiction rejection")
	}
}

// TestOperatorListWrongPassphraseErrors asserts a read against readable stores
// with the wrong passphrase surfaces non-zero, rather than printing an empty
// list that reads as "data gone".
func TestOperatorListWrongPassphraseErrors(t *testing.T) {
	operatorEnv(t)
	if out, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatalf("operator add: %v\n%s", err, out)
	}
	t.Setenv("VALISS_STORAGE_KEY", "the-wrong-passphrase")
	out, err := runCLI(t, "operator", "list")
	if err == nil {
		t.Errorf("operator list with the wrong passphrase succeeded; want non-zero exit\n%s", out)
	}
	if strings.Contains(out, "no operators") {
		t.Errorf("wrong-passphrase list printed \"no operators\" (reads as data-gone):\n%s", out)
	}
}

// TestOperatorListEmptyClean asserts a genuinely empty store directory stays a
// clean, zero-exit empty result, distinct from an unreadable-store failure.
func TestOperatorListEmptyClean(t *testing.T) {
	operatorEnv(t)
	out, err := runCLI(t, "operator", "list")
	if err != nil {
		t.Fatalf("empty operator list errored: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no operators") {
		t.Errorf("empty operator list = %q, want \"no operators\"", strings.TrimSpace(out))
	}
}

// TestWrongPassphraseMessageHasNoDriverTail asserts the wrong-passphrase message
// keeps the friendly prefix and does not trail a raw sqlite driver error.
func TestWrongPassphraseMessageHasNoDriverTail(t *testing.T) {
	operatorEnv(t)
	if out, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatalf("operator add: %v\n%s", err, out)
	}
	t.Setenv("VALISS_STORAGE_KEY", "the-wrong-passphrase")
	_, err := runCLI(t, "operator", "show", "acme")
	if err == nil {
		t.Fatal("operator show with the wrong passphrase succeeded; want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "wrong passphrase") {
		t.Errorf("message = %q, want the friendly passphrase reading", msg)
	}
	for _, tail := range []string{"sqlite", "not a database", "(26)"} {
		if strings.Contains(msg, tail) {
			t.Errorf("message leaks the raw driver tail %q: %q", tail, msg)
		}
	}
}

// TestBlankPathSegmentsRejected asserts entity names that are blank or contain
// whitespace are rejected before any store is touched.
func TestBlankPathSegmentsRejected(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "   "); err == nil {
		t.Error("operator add of a whitespace-only name succeeded; want rejection")
	}
	if out, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatalf("operator add: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "account", "add", "acme/te am"); err == nil {
		t.Error("account add with a whitespace name segment succeeded; want rejection")
	}
}

// TestHTTPBareSeparatorRejected asserts a bare --http value that is blank or a
// pure clause separator is refused rather than minting a garbage host.
func TestHTTPBareSeparatorRejected(t *testing.T) {
	for _, v := range []string{";", "a;b", "  "} {
		tokenEnv(t)
		if _, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", v); err == nil {
			t.Errorf("mint --http %q succeeded; want rejection", v)
		}
	}
}

// TestUnknownSubcommandExitsNonZero asserts a typo'd subcommand under a noun
// exits non-zero (matching an unknown root command), while a bare noun still
// prints help and exits zero.
func TestUnknownSubcommandExitsNonZero(t *testing.T) {
	operatorEnv(t)
	for _, noun := range []string{"operator", "account", "user", "template", "token", "creds", "allowlist", "store"} {
		if _, err := runCLI(t, noun, "frobnicate"); err == nil {
			t.Errorf("`%s frobnicate` returned nil; want non-zero exit on an unknown subcommand", noun)
		}
	}
	if _, err := runCLI(t, "token"); err != nil {
		t.Errorf("bare `token` errored: %v; want help and a zero exit", err)
	}
}

// TestAllowlistExportSingularPlural asserts the export header pluralizes the
// jti count with the shared helper.
func TestAllowlistExportSingularPlural(t *testing.T) {
	operatorEnv(t)
	if out, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatalf("operator add: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "allowlist", "add", "acme", "JTI-1"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "allowlist", "export", "acme")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(1 jti)") {
		t.Errorf("export header = %q, want it to read \"(1 jti)\"", out)
	}
	if _, err := runCLI(t, "allowlist", "add", "acme", "JTI-2"); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "allowlist", "export", "acme")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	if !strings.Contains(out, "(2 jtis)") {
		t.Errorf("export header = %q, want it to read \"(2 jtis)\"", out)
	}
}
