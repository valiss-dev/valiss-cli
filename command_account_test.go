package main

import (
	"strings"
	"testing"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
)

func TestAccountLifecycle(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatalf("operator add: %v", err)
	}

	if out, err := runCLI(t, "account", "add", "acme/team"); err != nil {
		t.Fatalf("account add: %v\n%s", err, out)
	}
	// Duplicate add fails.
	if _, err := runCLI(t, "account", "add", "acme/team"); err == nil {
		t.Error("duplicate account add succeeded; want failure")
	}
	// Adding under a missing operator fails.
	if _, err := runCLI(t, "account", "add", "ghost/team"); err == nil {
		t.Error("account add under missing operator succeeded; want failure")
	}

	out, err := runCLI(t, "account", "add", "acme/ops")
	if err != nil {
		t.Fatalf("account add ops: %v\n%s", err, out)
	}

	out, err = runCLI(t, "account", "list", "acme")
	if err != nil {
		t.Fatalf("account list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "acme/team") || !strings.Contains(out, "acme/ops") {
		t.Errorf("account list missing accounts\n%s", out)
	}

	out, err = runCLI(t, "account", "show", "acme/team", "--json")
	if err != nil {
		t.Fatalf("account show: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"kind": "account"`) || !strings.Contains(out, `"public_key": "A`) {
		t.Errorf("account show json unexpected\n%s", out)
	}

	// Remove cascades; show then fails.
	if out, err := runCLI(t, "account", "remove", "acme/team", "--yes"); err != nil {
		t.Fatalf("account remove: %v\n%s", err, out)
	}
	if _, err := runCLI(t, "account", "show", "acme/team"); err == nil {
		t.Error("show after remove succeeded; want failure")
	}
}

// TestAccountTokenIsOperatorSigned verifies the account token stored at
// account add is a real operator-signed account token.
func TestAccountTokenIsOperatorSigned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "pw")
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "account", "add", "acme/team"); err != nil {
		t.Fatal(err)
	}
	// Pull the operator public key and the account token out of the store
	// through the store package directly.
	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	op, err := st.LiveEntity("acme")
	if err != nil {
		t.Fatal(err)
	}
	acct, err := st.LiveEntity("acme/team")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := valiss.VerifyAccount(acct.Token, op.PublicKey)
	if err != nil {
		t.Fatalf("account token does not verify against the operator: %v", err)
	}
	if claims.Subject != acct.PublicKey {
		t.Errorf("account token subject = %q, want the account key %q", claims.Subject, acct.PublicKey)
	}
	if claims.Epoch != op.Epoch {
		t.Errorf("account token epoch = %d, want operator epoch %d", claims.Epoch, op.Epoch)
	}
}
