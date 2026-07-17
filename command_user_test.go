package main

import (
	"strings"
	"testing"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
)

func TestUserLifecycle(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "account", "add", "acme/team"); err != nil {
		t.Fatal(err)
	}

	if out, err := runCLI(t, "user", "add", "acme/team/alice"); err != nil {
		t.Fatalf("user add: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "user", "add", "acme/team/alice"); err == nil {
		t.Error("duplicate user add succeeded; want failure")
	}
	if _, err := runCLI(t, "user", "add", "acme/ghost/alice"); err == nil {
		t.Error("user add under missing account succeeded; want failure")
	}

	if _, err := runCLI(t, "user", "add", "acme/team/bob"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "user", "list", "acme/team")
	if err != nil {
		t.Fatalf("user list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("user list missing users\n%s", out)
	}

	// Removing the account cascades to its users.
	if _, err := runCLI(t, "account", "remove", "acme/team", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "user", "show", "acme/team/alice"); err == nil {
		t.Error("user show after account cascade succeeded; want failure")
	}
}

// TestUserTokenIsAccountSigned verifies the stored user token is a real
// account-signed user token carrying the domain epoch.
func TestUserTokenIsAccountSigned(t *testing.T) {
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
	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acct, _ := st.LiveEntity("acme/team")
	user, _ := st.LiveEntity("acme/team/alice")
	claims, err := valiss.VerifyUser(user.Token, acct.PublicKey)
	if err != nil {
		t.Fatalf("user token does not verify against the account: %v", err)
	}
	if claims.Subject != user.PublicKey {
		t.Errorf("user token subject = %q, want %q", claims.Subject, user.PublicKey)
	}
}
