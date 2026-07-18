package main

import (
	"strings"
	"testing"
)

// setupUser creates an operator/account/user in a fresh env for token tests.
func setupUser(t *testing.T) {
	t.Helper()
	operatorEnv(t)
	for _, args := range [][]string{
		{"operator", "add", "acme"},
		{"account", "add", "acme/team"},
		{"user", "add", "acme/team/alice"},
	} {
		if _, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestTokenMintFailClosed(t *testing.T) {
	setupUser(t)
	// An unqualified mint is rejected before anything is issued.
	if _, err := runCLI(t, "token", "mint", "acme/team/alice"); err == nil {
		t.Error("unqualified mint succeeded; want fail-closed error")
	}
	// --no-extension is the explicit opt-in and succeeds.
	if out, err := runCLI(t, "token", "mint", "acme/team/alice", "--no-extension"); err != nil {
		t.Fatalf("mint --no-extension: %v\n%s", err, out)
	}
}

func TestTokenMintExtensionUnion(t *testing.T) {
	setupUser(t)
	if _, err := runCLI(t, "template", "add", "acme/web", "--http", "api.example.com", "--ttl", "24h"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--template", "web", "--http", "www.example.com", "--grpc", "/svc.V1/*")
	if err != nil {
		t.Fatalf("mint: %v\n%s", err, out)
	}
	token := strings.SplitN(out, "\n", 2)[0]
	view, err := inspectToken(token)
	if err != nil {
		t.Fatalf("inspect minted token: %v", err)
	}
	if view.Type != "user" {
		t.Errorf("minted token type = %q, want user", view.Type)
	}
	http := string(view.Extensions["http"])
	if !strings.Contains(http, "api.example.com") || !strings.Contains(http, "www.example.com") {
		t.Errorf("http extension is not the union of template and flag hosts: %s", http)
	}
	if grpc := string(view.Extensions["grpc"]); !strings.Contains(grpc, "/svc.V1/*") {
		t.Errorf("grpc extension missing the flag method: %s", grpc)
	}
	if view.Expires == "" {
		t.Error("minted token has no expiry despite the template TTL")
	}
}

func TestTokenListShowRevoke(t *testing.T) {
	setupUser(t)
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com")
	if err != nil {
		t.Fatalf("mint: %v\n%s", err, out)
	}
	if !strings.Contains(out, "allowlisted: yes") {
		t.Errorf("mint did not report allowlisting by default:\n%s", out)
	}
	jti := jtiFromMintOutput(t, out)

	// list shows the live token.
	list, err := runCLI(t, "token", "list", "acme")
	if err != nil {
		t.Fatalf("token list: %v\n%s", err, list)
	}
	if !strings.Contains(list, jti) || !strings.Contains(list, "live") {
		t.Errorf("token list missing the live token:\n%s", list)
	}

	// show, then revoke, then status flips to revoked.
	if _, err := runCLI(t, "token", "show", "acme", jti); err != nil {
		t.Fatalf("token show: %v", err)
	}
	if _, err := runCLI(t, "token", "revoke", "acme", jti, "--yes"); err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	show, _ := runCLI(t, "token", "show", "acme", jti, "--json")
	if !strings.Contains(show, `"status": "revoked"`) {
		t.Errorf("token status not revoked after revoke:\n%s", show)
	}

	// Revoking an unknown jti fails.
	if _, err := runCLI(t, "token", "revoke", "acme", "nope", "--yes"); err == nil {
		t.Error("revoke of unknown jti succeeded; want failure")
	}
}

func TestTokenMintNoAllowlist(t *testing.T) {
	setupUser(t)
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com", "--no-allowlist")
	if err != nil {
		t.Fatalf("mint --no-allowlist: %v\n%s", err, out)
	}
	if strings.Contains(out, "allowlisted: yes") {
		t.Errorf("--no-allowlist still reported allowlisting:\n%s", out)
	}
}

// jtiFromMintOutput extracts the jti from mint's stderr metadata line.
func jtiFromMintOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "jti:"); i >= 0 {
			return strings.TrimSpace(line[i+len("jti:"):])
		}
	}
	t.Fatalf("no jti in mint output:\n%s", out)
	return ""
}
