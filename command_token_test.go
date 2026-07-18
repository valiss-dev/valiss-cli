package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
)

// tokenEnv sets up an operator/account/user chain and returns the store dir.
func tokenEnv(t *testing.T) {
	t.Helper()
	operatorEnv(t)
	for _, args := range [][]string{
		{"operator", "add", "acme"},
		{"account", "add", "acme/team"},
		{"user", "add", "acme/team/alice"},
	} {
		if out, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}

// jtiFromMint extracts the jti line from token mint output.
func jtiFromMint(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "jti:"); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("no jti in mint output:\n%s", out)
	return ""
}

// accountJTI reads the account's jti through account show --json.
func accountJTI(t *testing.T, path string) string {
	t.Helper()
	out, err := runCLI(t, "account", "show", path, "--json")
	if err != nil {
		t.Fatalf("account show %s: %v\n%s", path, err, out)
	}
	var s struct {
		JTI string `json:"jti"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("decode account show json: %v\n%s", err, out)
	}
	if s.JTI == "" {
		t.Fatalf("account show has no jti:\n%s", out)
	}
	return s.JTI
}

func TestTokenLifecycle(t *testing.T) {
	tokenEnv(t)

	// The allowlist keys on the account jti (deposited at account add), not the
	// user jti a mint produces.
	acctJTI := accountJTI(t, "acme/team")
	if al, err := runCLI(t, "allowlist", "list", "acme"); err != nil || !strings.Contains(al, acctJTI) {
		t.Errorf("allowlist list = %q (err %v), want account jti %s", al, err, acctJTI)
	}

	// An unqualified mint fails closed.
	if _, err := runCLI(t, "token", "mint", "acme/team/alice"); err == nil {
		t.Error("unqualified mint succeeded; want fail-closed error")
	}

	// A user mint does not touch the allowlist: the user jti is never listed.
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com")
	if err != nil {
		t.Fatalf("token mint: %v\n%s", err, out)
	}
	userJTI := jtiFromMint(t, out)
	if al, err := runCLI(t, "allowlist", "list", "acme"); err != nil || strings.Contains(al, userJTI) {
		t.Errorf("user mint registered its jti in the allowlist: %q", al)
	}

	// token list is subtree-scoped and shows the user issuance and the account.
	if lst, err := runCLI(t, "token", "list", "acme"); err != nil || !strings.Contains(lst, userJTI) || !strings.Contains(lst, acctJTI) {
		t.Errorf("token list = %q (err %v), want %s and %s", lst, err, userJTI, acctJTI)
	}

	// token show carries the token blob.
	if show, err := runCLI(t, "token", "show", "acme", userJTI); err != nil || !strings.Contains(show, "token:") {
		t.Errorf("token show = %q (err %v)", show, err)
	}

	// Revoking a user jti is refused: it is not enforceable in v0.13.1.
	if _, err := runCLI(t, "token", "revoke", "acme", userJTI, "--yes"); err == nil {
		t.Error("user-jti revoke succeeded; want the not-supported refusal")
	}

	// Revoking the account jti removes it from the allowlist (the enforced
	// revocation) and cuts the account and its users.
	if rev, err := runCLI(t, "token", "revoke", "acme", acctJTI, "--yes"); err != nil || !strings.Contains(rev, "Revoked") {
		t.Errorf("account revoke = %q (err %v)", rev, err)
	}
	if al, err := runCLI(t, "allowlist", "list", "acme"); err != nil || strings.Contains(al, acctJTI) {
		t.Errorf("allowlist still contains revoked account jti: %q", al)
	}
	if show, err := runCLI(t, "token", "show", "acme", acctJTI); err != nil || !strings.Contains(show, "revoked") {
		t.Errorf("token show after revoke = %q (err %v), want revoked status", show, err)
	}
	// A second account revoke is a no-op, not an error.
	if out, err := runCLI(t, "token", "revoke", "acme", acctJTI, "--yes"); err != nil || !strings.Contains(out, "already revoked") {
		t.Errorf("second revoke = %q (err %v)", out, err)
	}
}

func TestAccountAddNoAllowlist(t *testing.T) {
	operatorEnv(t)
	for _, args := range [][]string{{"operator", "add", "acme"}, {"account", "add", "acme/team", "--no-allowlist"}} {
		if out, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	jti := accountJTI(t, "acme/team")
	if al, err := runCLI(t, "allowlist", "list", "acme"); err != nil || strings.Contains(al, jti) {
		t.Errorf("--no-allowlist account add still registered jti %s: %q (err %v)", jti, al, err)
	}
}

func TestAccountAddRegistersAllowlist(t *testing.T) {
	operatorEnv(t)
	for _, args := range [][]string{{"operator", "add", "acme"}, {"account", "add", "acme/team"}} {
		if out, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	jti := accountJTI(t, "acme/team")
	if al, err := runCLI(t, "allowlist", "list", "acme"); err != nil || !strings.Contains(al, jti) {
		t.Errorf("account add did not register account jti %s: %q (err %v)", jti, al, err)
	}
}

// TestTokenMintVerifiable confirms a minted token is a real account-signed user
// token carrying the http grant as the httpauth claim shape.
func TestTokenMintVerifiable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "pw")
	for _, args := range [][]string{
		{"operator", "add", "acme"},
		{"account", "add", "acme/team"},
		{"user", "add", "acme/team/alice"},
	} {
		if _, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if _, err := runCLI(t, "token", "mint", "acme/team/alice",
		"--http", "hosts=example.com;methods=GET,POST;paths=/v1/*"); err != nil {
		t.Fatalf("mint: %v", err)
	}

	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acct, _ := st.LiveEntity("acme/team")
	recs, err := st.ListTokens("acme/team/alice")
	if err != nil || len(recs) != 1 {
		t.Fatalf("ListTokens = %d recs (err %v), want 1", len(recs), err)
	}
	claims, err := valiss.VerifyUser(recs[0].Token, acct.PublicKey)
	if err != nil {
		t.Fatalf("minted token does not verify against the account: %v", err)
	}
	ext, ok, err := valiss.ExtOf[httpExt](claims.Ext)
	if err != nil || !ok {
		t.Fatalf("http extension missing: ok=%v err=%v", ok, err)
	}
	if len(ext.Hosts) != 1 || ext.Hosts[0] != "example.com" {
		t.Errorf("ext.Hosts = %v, want [example.com]", ext.Hosts)
	}
	if len(ext.Methods) != 2 || len(ext.Paths) != 1 {
		t.Errorf("ext methods/paths = %v/%v, want 2/1", ext.Methods, ext.Paths)
	}
}

func TestTokenMintExt(t *testing.T) {
	extFile := filepath.Join(t.TempDir(), "quota.json")
	if err := os.WriteFile(extFile, []byte(`{"rps":250}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		arg     string
		wantErr bool
	}{
		{"inline json", `quota={"rps":100}`, false},
		{"from file", "quota=@" + extFile, false},
		{"invalid json", "quota=not-json", true},
		{"reserved name http", `http={"x":1}`, true},
		{"reserved name gen", `gen={"x":1}`, true},
		{"missing equals", "quota", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokenEnv(t)
			_, err := runCLI(t, "token", "mint", "acme/team/alice", "--ext", c.arg)
			if (err != nil) != c.wantErr {
				t.Fatalf("mint --ext %q error = %v, wantErr %v", c.arg, err, c.wantErr)
			}
		})
	}
}

func TestTokenMintExtDuplicate(t *testing.T) {
	tokenEnv(t)
	_, err := runCLI(t, "token", "mint", "acme/team/alice",
		"--ext", `quota={"rps":1}`, "--ext", `quota={"rps":2}`)
	if err == nil {
		t.Error("duplicate --ext name succeeded; want rejection")
	}
}

func TestTokenMintTemplate(t *testing.T) {
	tokenEnv(t)
	if _, err := runCLI(t, "template", "add", "acme/web", "--http", "example.com", "--ttl", "1h"); err != nil {
		t.Fatalf("template add: %v", err)
	}
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--template", "web")
	if err != nil {
		t.Fatalf("mint --template: %v\n%s", err, out)
	}
	jti := jtiFromMint(t, out)
	show, err := runCLI(t, "token", "show", "acme", jti)
	if err != nil || !strings.Contains(show, "web@1") {
		t.Errorf("token show = %q (err %v), want template stamp web@1", show, err)
	}
}
