package main

import (
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// findCommand resolves a command path against the root, failing the test if
// the path does not resolve to a leaf of that exact name.
func findCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd, rest, err := newRootCommand().Find(path)
	if err != nil {
		t.Fatalf("Find(%v): %v", path, err)
	}
	if len(rest) != 0 {
		t.Fatalf("Find(%v): unresolved trailing args %v (stopped at %q)", path, rest, cmd.Name())
	}
	if want := path[len(path)-1]; cmd.Name() != want {
		t.Fatalf("Find(%v): resolved to %q, want %q", path, cmd.Name(), want)
	}
	return cmd
}

// TestTreeShape asserts that every command path specified by ADR 0021
// resolves.
func TestTreeShape(t *testing.T) {
	paths := [][]string{
		{"operator", "add"}, {"operator", "list"}, {"operator", "show"},
		{"operator", "rotate"}, {"operator", "remove"}, {"operator", "audit"},

		{"account", "add"}, {"account", "list"}, {"account", "show"},
		{"account", "remove"}, {"account", "audit"},

		{"user", "add"}, {"user", "list"}, {"user", "show"},
		{"user", "remove"}, {"user", "audit"},

		{"template", "add"}, {"template", "list"}, {"template", "show"},
		{"template", "remove"}, {"template", "audit"},

		{"token", "mint"}, {"token", "list"}, {"token", "show"},
		{"token", "revoke"},

		{"creds", "export"},

		{"allowlist", "list"}, {"allowlist", "add"},
		{"allowlist", "remove"}, {"allowlist", "export"},

		{"store", "init"}, {"store", "info"}, {"store", "config"},

		{"inspect"},
	}
	for _, p := range paths {
		findCommand(t, p...)
	}
}

// TestNoHiddenContext guards the core ADR 0021 decision: there is no ambient
// operator/account/context selection carried as a persistent flag.
func TestNoHiddenContext(t *testing.T) {
	root := newRootCommand()
	for _, name := range []string{"operator", "account", "user", "context", "current"} {
		if f := root.PersistentFlags().Lookup(name); f != nil {
			t.Errorf("root carries a persistent %q flag; addressing must be explicit per path", name)
		}
	}
}

// TestFlagPresence asserts each command carries the flags ADR 0021 gives it.
func TestFlagPresence(t *testing.T) {
	cases := []struct {
		path  []string
		flags []string
	}{
		// --json on list and show (and the audit reads).
		{[]string{"operator", "list"}, []string{"json"}},
		{[]string{"operator", "show"}, []string{"json"}},
		{[]string{"operator", "audit"}, []string{"json"}},
		{[]string{"account", "list"}, []string{"json"}},
		{[]string{"account", "show"}, []string{"json"}},
		{[]string{"user", "list"}, []string{"json"}},
		{[]string{"user", "show"}, []string{"json"}},
		{[]string{"template", "list"}, []string{"json"}},
		{[]string{"template", "show"}, []string{"json"}},
		{[]string{"token", "list"}, []string{"json"}},
		{[]string{"token", "show"}, []string{"json"}},
		{[]string{"allowlist", "list"}, []string{"json"}},
		{[]string{"store", "info"}, []string{"json"}},
		{[]string{"inspect"}, []string{"json"}},

		// --yes on destructive commands.
		{[]string{"operator", "rotate"}, []string{"yes"}},
		{[]string{"operator", "remove"}, []string{"yes"}},
		{[]string{"account", "remove"}, []string{"yes"}},
		{[]string{"user", "remove"}, []string{"yes"}},
		{[]string{"template", "remove"}, []string{"yes"}},
		{[]string{"token", "revoke"}, []string{"yes"}},
		{[]string{"allowlist", "remove"}, []string{"yes"}},

		// Grant and issuance flags on mint.
		{[]string{"token", "mint"}, []string{"template", "http", "grpc", "custom", "ttl", "no-extension", "no-allowlist"}},

		// Claimset flags on template add.
		{[]string{"template", "add"}, []string{"http", "grpc", "custom", "ttl", "bearer", "description"}},

		// Creds kinds.
		{[]string{"creds", "export"}, []string{"bundle", "bearer"}},

		// Store init retention.
		{[]string{"store", "init"}, []string{"audit-retention"}},
	}
	for _, c := range cases {
		cmd := findCommand(t, c.path...)
		for _, name := range c.flags {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("%v: missing flag --%s", c.path, name)
			}
		}
	}
}

// TestAuditRetentionDefault asserts store init defaults to ninety days.
func TestAuditRetentionDefault(t *testing.T) {
	cmd := findCommand(t, "store", "init")
	got, err := cmd.Flags().GetDuration("audit-retention")
	if err != nil {
		t.Fatalf("GetDuration: %v", err)
	}
	if want := 2160 * time.Hour; got != want {
		t.Errorf("audit-retention default = %v, want %v", got, want)
	}
}

// TestPathArity exercises the positional-argument validators.
func TestPathArity(t *testing.T) {
	cases := []struct {
		name    string
		path    []string
		args    []string
		wantErr bool
	}{
		{"operator add ok", []string{"operator", "add"}, []string{"op"}, false},
		{"operator add empty", []string{"operator", "add"}, []string{}, true},
		{"operator add too deep", []string{"operator", "add"}, []string{"op/acct"}, true},
		{"operator add extra arg", []string{"operator", "add"}, []string{"op", "x"}, true},

		{"account add ok", []string{"account", "add"}, []string{"op/acct"}, false},
		{"account add too shallow", []string{"account", "add"}, []string{"op"}, true},
		{"account add too deep", []string{"account", "add"}, []string{"op/acct/user"}, true},

		{"user add ok", []string{"user", "add"}, []string{"op/acct/user"}, false},
		{"user add too shallow", []string{"user", "add"}, []string{"op/acct"}, true},

		{"token mint ok", []string{"token", "mint"}, []string{"op/acct/user"}, false},
		{"token mint too shallow", []string{"token", "mint"}, []string{"op/acct"}, true},

		{"token show ok", []string{"token", "show"}, []string{"op", "jti-1"}, false},
		{"token show missing jti", []string{"token", "show"}, []string{"op"}, true},
		{"token show path too deep", []string{"token", "show"}, []string{"op/acct", "jti-1"}, true},
		{"token show extra", []string{"token", "show"}, []string{"op", "jti-1", "x"}, true},

		{"token list operator", []string{"token", "list"}, []string{"op"}, false},
		{"token list account", []string{"token", "list"}, []string{"op/acct"}, false},
		{"token list user", []string{"token", "list"}, []string{"op/acct/user"}, false},
		{"token list too deep", []string{"token", "list"}, []string{"op/acct/user/extra"}, true},

		{"allowlist add ok", []string{"allowlist", "add"}, []string{"op", "jti-1"}, false},
		{"allowlist add missing jti", []string{"allowlist", "add"}, []string{"op"}, true},

		{"creds export operator", []string{"creds", "export"}, []string{"op"}, false},
		{"creds export user", []string{"creds", "export"}, []string{"op/acct/user"}, false},
		{"creds export too deep", []string{"creds", "export"}, []string{"op/acct/user/x"}, true},
		{"creds export empty", []string{"creds", "export"}, []string{}, true},

		{"inspect ok", []string{"inspect"}, []string{"token-blob"}, false},
		{"inspect empty", []string{"inspect"}, []string{}, true},
		{"inspect extra", []string{"inspect"}, []string{"a", "b"}, true},

		{"empty leading segment", []string{"account", "add"}, []string{"/acct"}, true},
		{"empty trailing segment", []string{"account", "add"}, []string{"op/"}, true},
		{"empty middle segment", []string{"user", "add"}, []string{"op//user"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := findCommand(t, c.path...)
			err := cmd.Args(cmd, c.args)
			if (err != nil) != c.wantErr {
				t.Errorf("Args(%v) error = %v, wantErr %v", c.args, err, c.wantErr)
			}
		})
	}
}

// TestValidateMintFlags covers the fail-closed extension rule directly.
func TestValidateMintFlags(t *testing.T) {
	cases := []struct {
		name        string
		hasTemplate bool
		noExtension bool
		grantCount  int
		wantErr     bool
	}{
		{"unqualified rejected", false, false, 0, true},
		{"template only", true, false, 0, false},
		{"grants only", false, false, 1, false},
		{"no-extension only", false, true, 0, false},
		{"template unions with grants", true, false, 2, false},
		{"no-extension with template rejected", true, true, 0, true},
		{"no-extension with grants rejected", false, true, 1, true},
		{"no-extension with template and grants rejected", true, true, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateMintFlags(c.hasTemplate, c.noExtension, c.grantCount)
			if (err != nil) != c.wantErr {
				t.Errorf("validateMintFlags(%v,%v,%d) error = %v, wantErr %v",
					c.hasTemplate, c.noExtension, c.grantCount, err, c.wantErr)
			}
		})
	}
}

// TestMintPreRunE wires the fail-closed rule end to end through the mint
// command's flags: valid combinations pass PreRunE and reach the stub error,
// invalid ones are rejected before any body runs.
func TestMintPreRunE(t *testing.T) {
	cases := []struct {
		name    string
		flags   map[string]string
		wantErr bool
	}{
		{"unqualified", nil, true},
		{"template", map[string]string{"template": "web"}, false},
		{"template pinned", map[string]string{"template": "web@2"}, false},
		{"http grant", map[string]string{"http": "example.com"}, false},
		{"grpc grant", map[string]string{"grpc": "svc.example.com"}, false},
		{"custom grant", map[string]string{"custom": "app.example.com"}, false},
		{"no-extension", map[string]string{"no-extension": "true"}, false},
		{"template and grant union", map[string]string{"template": "web", "http": "example.com"}, false},
		{"no-extension with grant", map[string]string{"no-extension": "true", "http": "example.com"}, true},
		{"no-extension with template", map[string]string{"no-extension": "true", "template": "web"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A fresh tree per case: flag "changed" state must not leak.
			mint := findCommand(t, "token", "mint")
			for name, val := range c.flags {
				if err := mint.Flags().Set(name, val); err != nil {
					t.Fatalf("set --%s=%s: %v", name, val, err)
				}
			}
			err := mint.PreRunE(mint, []string{"op/acct/user"})
			if (err != nil) != c.wantErr {
				t.Fatalf("PreRunE error = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			// A qualified mint proceeds to the shared stub.
			if runErr := mint.RunE(mint, []string{"op/acct/user"}); !errors.Is(runErr, errNotImplemented) {
				t.Errorf("RunE = %v, want errNotImplemented", runErr)
			}
		})
	}
}

// TestStoreConfig covers the git-style get/set argument shape: list, set,
// and the rejections (lone key, unknown key, unparseable value, arity).
func TestStoreConfig(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"list all parameters", []string{"op"}, false},
		{"set a valid key/value", []string{"op", "audit-retention", "720h"}, false},
		{"lone key rejected", []string{"op", "audit-retention"}, true},
		{"unknown key rejected", []string{"op", "bogus-key", "5"}, true},
		{"unparseable duration rejected", []string{"op", "audit-retention", "not-a-duration"}, true},
		{"trailing arg rejected", []string{"op", "audit-retention", "720h", "extra"}, true},
		{"bad operator path rejected", []string{"op/acct", "audit-retention", "720h"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := findCommand(t, "store", "config")
			err := cmd.Args(cmd, c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("Args(%v) error = %v, wantErr %v", c.args, err, c.wantErr)
			}
			// The store-config body is now implemented and needs a real store
			// and passphrase; the argument-shape validation above is what this
			// test covers. End-to-end config behavior is exercised by the
			// store package's own tests.
		})
	}
}

// TestStubsReturnNotImplemented spot-checks that leaf bodies are stubs.
func TestStubsReturnNotImplemented(t *testing.T) {
	// inspect, store, operator, account, and user verbs are implemented; the
	// rest remain stubs until their store-backed bodies land.
	for _, path := range [][]string{
		{"template", "add"}, {"token", "revoke"}, {"creds", "export"},
		{"allowlist", "export"},
	} {
		cmd := findCommand(t, path...)
		if err := cmd.RunE(cmd, nil); !errors.Is(err, errNotImplemented) {
			t.Errorf("%v RunE = %v, want errNotImplemented", path, err)
		}
	}
}
