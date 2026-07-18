package main

import (
	"testing"

	"valiss.dev/valiss/creds"
)

func credsEnv(t *testing.T) {
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

func TestCredsExport(t *testing.T) {
	credsEnv(t)

	cases := []struct {
		name        string
		args        []string
		wantAccount bool
		wantUser    bool
		wantSeed    bool
		wantErr     bool
	}{
		{"account", []string{"acme/team"}, true, false, true, false},
		{"user", []string{"acme/team/alice"}, false, true, true, false},
		{"bundle", []string{"acme/team/alice", "--bundle"}, true, true, true, false},
		// --bearer is refused unless the current token was minted as a bearer
		// token; alice's current token here is the plain user-add token.
		{"bearer non-bearer current rejected", []string{"acme/team/alice", "--bearer"}, false, false, false, true},
		{"bearer bundle non-bearer current rejected", []string{"acme/team/alice", "--bundle", "--bearer"}, false, false, false, true},
		{"account bearer rejected", []string{"acme/team", "--bearer"}, false, false, false, true},
		{"operator rejected", []string{"acme"}, false, false, false, true},
		{"account bundle rejected", []string{"acme/team", "--bundle"}, false, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := runCLI(t, append([]string{"creds", "export"}, c.args...)...)
			if (err != nil) != c.wantErr {
				t.Fatalf("creds export %v error = %v, wantErr %v\n%s", c.args, err, c.wantErr, out)
			}
			if c.wantErr {
				return
			}
			b, err := creds.Parse(out)
			if err != nil {
				t.Fatalf("creds.Parse: %v\n%s", err, out)
			}
			if (b.AccountToken != "") != c.wantAccount {
				t.Errorf("account token present = %v, want %v", b.AccountToken != "", c.wantAccount)
			}
			if (b.UserToken != "") != c.wantUser {
				t.Errorf("user token present = %v, want %v", b.UserToken != "", c.wantUser)
			}
			if (len(b.Seed) > 0) != c.wantSeed {
				t.Errorf("seed present = %v, want %v", len(b.Seed) > 0, c.wantSeed)
			}
		})
	}
}

// TestCredsExportServesCurrentToken is the core of the model change: creds
// export serves the entity's current token, which 'token mint' re-issues. A
// mint that adds an http grant must show up in the exported user creds.
func TestCredsExportServesCurrentToken(t *testing.T) {
	credsEnv(t)

	// Before any mint, the exported user token carries no http grant.
	out, err := runCLI(t, "creds", "export", "acme/team/alice")
	if err != nil {
		t.Fatalf("creds export: %v\n%s", err, out)
	}
	b, err := creds.Parse(out)
	if err != nil {
		t.Fatalf("creds.Parse: %v\n%s", err, out)
	}
	if v, err := inspectToken(b.UserToken); err != nil || len(v.Extensions) != 0 {
		t.Fatalf("pre-mint user token unexpectedly carries extensions: %v (err %v)", v.Extensions, err)
	}

	// Mint an http grant, then export again: the current token now carries it.
	if out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "api.example.com"); err != nil {
		t.Fatalf("token mint: %v\n%s", err, out)
	}
	out, err = runCLI(t, "creds", "export", "acme/team/alice")
	if err != nil {
		t.Fatalf("creds export after mint: %v\n%s", err, out)
	}
	b, err = creds.Parse(out)
	if err != nil {
		t.Fatalf("creds.Parse: %v\n%s", err, out)
	}
	v, err := inspectToken(b.UserToken)
	if err != nil {
		t.Fatalf("inspect exported user token: %v", err)
	}
	if _, ok := v.Extensions["http"]; !ok {
		t.Errorf("exported user token does not carry the minted http grant: ext=%v", v.Extensions)
	}
	if len(b.Seed) == 0 {
		t.Error("default user creds carry no seed")
	}
}

// TestCredsExportBearerAfterMint covers the reconciled bearer path: after
// 'token mint --bearer', the current token is a bearer token and bearer creds
// export succeeds, carrying the user token and no seed.
func TestCredsExportBearerAfterMint(t *testing.T) {
	credsEnv(t)

	if out, err := runCLI(t, "token", "mint", "acme/team/alice", "--bearer", "--http", "api.example.com"); err != nil {
		t.Fatalf("token mint --bearer: %v\n%s", err, out)
	}
	out, err := runCLI(t, "creds", "export", "acme/team/alice", "--bearer")
	if err != nil {
		t.Fatalf("creds export --bearer after bearer mint: %v\n%s", err, out)
	}
	b, err := creds.Parse(out)
	if err != nil {
		t.Fatalf("creds.Parse: %v\n%s", err, out)
	}
	if b.UserToken == "" {
		t.Error("bearer creds carry no user token")
	}
	if len(b.Seed) != 0 {
		t.Error("bearer creds carry a seed; they must not")
	}
	if v, err := inspectToken(b.UserToken); err != nil || !v.Bearer {
		t.Errorf("exported bearer creds token is not a bearer token: bearer=%v err=%v", v.Bearer, err)
	}
}
