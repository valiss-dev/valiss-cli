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
		{"bearer user", []string{"acme/team/alice", "--bearer"}, false, true, false, false},
		{"bearer bundle", []string{"acme/team/alice", "--bundle", "--bearer"}, true, true, false, false},
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
