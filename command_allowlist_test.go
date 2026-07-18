package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"valiss.dev/valiss"
)

func TestAllowlistLifecycle(t *testing.T) {
	setupUser(t)
	// Minting deposits the jti by default.
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com")
	if err != nil {
		t.Fatalf("mint: %v\n%s", err, out)
	}
	jti := jtiFromMintOutput(t, out)

	list, _ := runCLI(t, "allowlist", "list", "acme")
	if !strings.Contains(list, jti) {
		t.Errorf("mint jti not in allowlist:\n%s", list)
	}

	// Manual add is idempotent.
	if _, err := runCLI(t, "allowlist", "add", "acme", "MANUAL"); err != nil {
		t.Fatal(err)
	}
	dup, _ := runCLI(t, "allowlist", "add", "acme", "MANUAL")
	if !strings.Contains(dup, "already allowlisted") {
		t.Errorf("duplicate add not idempotent:\n%s", dup)
	}

	// Remove, then removing again fails.
	if _, err := runCLI(t, "allowlist", "remove", "acme", "MANUAL", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "allowlist", "remove", "acme", "MANUAL", "--yes"); err == nil {
		t.Error("removing an absent jti succeeded; want failure")
	}
}

// TestAllowlistExportLoads round-trips the export through the library's
// LoadAllowlistFile: the exported file must be exactly what servers consume.
func TestAllowlistExportLoads(t *testing.T) {
	setupUser(t)
	out, err := runCLI(t, "token", "mint", "acme/team/alice", "--http", "example.com")
	if err != nil {
		t.Fatalf("mint: %v\n%s", err, out)
	}
	jti := jtiFromMintOutput(t, out)

	export, err := runCLI(t, "allowlist", "export", "acme")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, export)
	}
	path := filepath.Join(t.TempDir(), "allowlist.txt")
	if err := os.WriteFile(path, []byte(export), 0o600); err != nil {
		t.Fatal(err)
	}
	al, err := valiss.LoadAllowlistFile(path)
	if err != nil {
		t.Fatalf("valiss.LoadAllowlistFile could not parse the export: %v", err)
	}
	if !al.Allowed(jti) {
		t.Errorf("exported allowlist does not allow the minted jti %s", jti)
	}
	if al.Allowed("not-in-the-list") {
		t.Error("exported allowlist allowed an unlisted jti")
	}
}
