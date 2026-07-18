package main

import (
	"strings"
	"testing"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
)

// operatorEnv points the CLI at a temp store directory with a fixed passphrase.
func operatorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VALISS_STORE_DIR", t.TempDir())
	t.Setenv("VALISS_STORAGE_KEY", "test-passphrase")
}

func TestOperatorLifecycle(t *testing.T) {
	operatorEnv(t)

	out, err := runCLI(t, "operator", "add", "acme")
	if err != nil {
		t.Fatalf("operator add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Added operator") {
		t.Errorf("add output unexpected:\n%s", out)
	}

	// A second add of the same operator fails.
	if _, err := runCLI(t, "operator", "add", "acme"); err == nil {
		t.Error("second operator add succeeded; want failure")
	}

	out, err = runCLI(t, "operator", "show", "acme", "--json")
	if err != nil {
		t.Fatalf("operator show: %v\n%s", err, out)
	}
	for _, want := range []string{`"kind": "operator"`, `"epoch": 1`, `"public_key": "O`} {
		if !strings.Contains(out, want) {
			t.Errorf("show json missing %q\n%s", want, out)
		}
	}

	// Rotate advances the epoch.
	if out, err := runCLI(t, "operator", "rotate", "acme", "--yes"); err != nil {
		t.Fatalf("operator rotate: %v\n%s", err, out)
	}
	out, err = runCLI(t, "operator", "show", "acme", "--json")
	if err != nil {
		t.Fatalf("operator show after rotate: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"epoch": 2`) || !strings.Contains(out, `"generation": 2`) {
		t.Errorf("rotate did not advance epoch/generation\n%s", out)
	}

	// Audit shows the add and rotate under the subtree.
	out, err = runCLI(t, "operator", "audit", "acme")
	if err != nil {
		t.Fatalf("operator audit: %v\n%s", err, out)
	}
	for _, want := range []string{"entity.add", "operator.rotate", "store.init"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit missing %q\n%s", want, out)
		}
	}

	// Remove, then show fails.
	if out, err := runCLI(t, "operator", "remove", "acme", "--yes"); err != nil {
		t.Fatalf("operator remove: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "operator", "show", "acme"); err == nil {
		t.Error("show after remove succeeded; want failure")
	}
}

func TestOperatorList(t *testing.T) {
	operatorEnv(t)
	for _, name := range []string{"acme", "beta"} {
		if out, err := runCLI(t, "operator", "add", name); err != nil {
			t.Fatalf("add %s: %v\n%s", name, err, out)
		}
	}
	out, err := runCLI(t, "operator", "list")
	if err != nil {
		t.Fatalf("operator list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "beta") {
		t.Errorf("list missing operators\n%s", out)
	}
}

// TestOperatorTokenExport asserts the exported operator token is exactly what
// valiss-go's WithOperatorToken consumes: self-signed by the operator key and
// carrying the current epoch.
func TestOperatorTokenExport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "pw")
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "operator", "token", "acme")
	if err != nil {
		t.Fatalf("operator token: %v\n%s", err, out)
	}
	token := strings.TrimSpace(out)

	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	op, err := st.LiveEntity("acme")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := valiss.VerifyOperator(token, op.PublicKey)
	if err != nil {
		t.Fatalf("exported operator token does not verify (WithOperatorToken would reject it): %v", err)
	}
	if claims.Epoch != op.Epoch {
		t.Errorf("operator token epoch = %d, want %d", claims.Epoch, op.Epoch)
	}
}

// TestRotateReissuesDomain asserts the rotation ceremony re-issues accounts and
// users at the new epoch and swaps the account's allowlist entry to the new jti,
// so the rotated domain is usable under an operator-token policy.
func TestRotateReissuesDomain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "pw")
	for _, args := range [][]string{
		{"operator", "add", "acme"},
		{"account", "add", "acme/team"},
		{"user", "add", "acme/team/alice"},
	} {
		if out, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	oldAcctJTI := accountJTI(t, "acme/team")
	if out, err := runCLI(t, "operator", "rotate", "acme", "--yes"); err != nil {
		t.Fatalf("rotate: %v\n%s", err, out)
	}

	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	op, _ := st.LiveEntity("acme")
	acct, _ := st.LiveEntity("acme/team")
	user, _ := st.LiveEntity("acme/team/alice")
	if op.Epoch != 2 || acct.Epoch != 2 || user.Epoch != 2 {
		t.Fatalf("epochs after rotate: op=%d acct=%d user=%d, want all 2", op.Epoch, acct.Epoch, user.Epoch)
	}
	// The re-issued account token verifies against the operator and echoes epoch 2.
	ac, err := valiss.VerifyAccount(acct.Token, op.PublicKey)
	if err != nil || ac.Epoch != 2 {
		t.Fatalf("re-issued account token: epoch=%d err=%v, want epoch 2 and no error", ac.Epoch, err)
	}
	// The re-issued user token verifies against the account and echoes epoch 2.
	uc, err := valiss.VerifyUser(user.Token, acct.PublicKey)
	if err != nil || uc.Epoch != 2 {
		t.Fatalf("re-issued user token: epoch=%d err=%v, want epoch 2 and no error", uc.Epoch, err)
	}
	// The allowlist swapped: old account jti out, new account jti in.
	newAcctJTI := ac.ID
	if oldPresent, _ := st.AllowlistContains(oldAcctJTI); oldPresent {
		t.Errorf("old account jti %s still in allowlist after rotation", oldAcctJTI)
	}
	if newPresent, _ := st.AllowlistContains(newAcctJTI); !newPresent {
		t.Errorf("new account jti %s not in allowlist after rotation", newAcctJTI)
	}
}

func TestOperatorKeyStableAcrossRotation(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	before, _ := runCLI(t, "operator", "show", "acme", "--json")
	if _, err := runCLI(t, "operator", "rotate", "acme", "--yes"); err != nil {
		t.Fatal(err)
	}
	after, _ := runCLI(t, "operator", "show", "acme", "--json")

	keyOf := func(s string) string {
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, `"public_key"`) {
				return line
			}
		}
		return ""
	}
	if keyOf(before) != keyOf(after) || keyOf(before) == "" {
		t.Errorf("operator key changed across rotation (epoch rotation must keep the key)\nbefore: %s\nafter:  %s", keyOf(before), keyOf(after))
	}
}
