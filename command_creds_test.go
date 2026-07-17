package main

import (
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
	"valiss.dev/valiss/creds"
)

func TestCredsExportAccount(t *testing.T) {
	setupUser(t) // operator acme, account acme/team, user acme/team/alice
	out, err := runCLI(t, "creds", "export", "acme/team")
	if err != nil {
		t.Fatalf("creds export account: %v\n%s", err, out)
	}
	parsed, err := creds.Parse(out)
	if err != nil {
		t.Fatalf("creds.Parse rejected the account creds: %v\n%s", err, out)
	}
	if parsed.AccountToken == "" || len(parsed.Seed) == 0 || parsed.UserToken != "" {
		t.Fatalf("account creds shape wrong: account=%t seed=%t user=%t",
			parsed.AccountToken != "", len(parsed.Seed) > 0, parsed.UserToken != "")
	}
	// The seed derives the account key the token names.
	claims, err := valiss.Decode(parsed.AccountToken)
	if err != nil {
		t.Fatal(err)
	}
	kp, err := nkeys.FromSeed(parsed.Seed)
	if err != nil {
		t.Fatalf("seed does not parse: %v", err)
	}
	pub, _ := kp.PublicKey()
	if pub != claims.Subject {
		t.Errorf("seed derives %s, not the token subject %s", pub, claims.Subject)
	}
}

func TestCredsExportUserBundleVerifies(t *testing.T) {
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
	out, err := runCLI(t, "creds", "export", "acme/team/alice", "--bundle")
	if err != nil {
		t.Fatalf("creds export bundle: %v\n%s", err, out)
	}
	parsed, err := creds.Parse(out)
	if err != nil {
		t.Fatalf("creds.Parse rejected the bundle: %v\n%s", err, out)
	}
	if parsed.AccountToken == "" || parsed.UserToken == "" || len(parsed.Seed) == 0 {
		t.Fatal("bundle should carry the account token, user token, and user seed")
	}

	// Verify the whole chain: operator -> account -> user.
	st, err := store.Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	op, _ := st.LiveEntity("acme")
	acctClaims, err := valiss.VerifyAccount(parsed.AccountToken, op.PublicKey)
	if err != nil {
		t.Fatalf("bundle account token does not verify against the operator: %v", err)
	}
	if _, err := valiss.VerifyUser(parsed.UserToken, acctClaims.Subject); err != nil {
		t.Fatalf("bundle user token does not verify against the account: %v", err)
	}
}

func TestCredsExportRejections(t *testing.T) {
	setupUser(t)
	if _, err := runCLI(t, "creds", "export", "acme"); err == nil {
		t.Error("operator-level creds export succeeded; want rejection")
	}
	if _, err := runCLI(t, "creds", "export", "acme/team", "--bundle"); err == nil {
		t.Error("--bundle at account level succeeded; want rejection")
	}
}

func TestCredsExportBearerOmitsSeed(t *testing.T) {
	setupUser(t)
	out, err := runCLI(t, "creds", "export", "acme/team/alice", "--bearer")
	if err != nil {
		t.Fatalf("creds export bearer: %v\n%s", err, out)
	}
	if strings.Contains(out, "VALISS SEED") {
		t.Errorf("bearer creds must not carry a seed:\n%s", out)
	}
}
