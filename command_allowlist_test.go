package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"valiss.dev/valiss"
)

func TestAllowlistLifecycle(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}

	// An empty allowlist lists nothing.
	if out, err := runCLI(t, "allowlist", "list", "acme"); err != nil || !strings.Contains(out, "none") {
		t.Errorf("empty allowlist list = %q (err %v)", out, err)
	}

	// Add is a first-class operation over arbitrary jtis.
	if out, err := runCLI(t, "allowlist", "add", "acme", "JTI-EXTERNAL"); err != nil || !strings.Contains(out, "Added") {
		t.Errorf("allowlist add = %q (err %v)", out, err)
	}
	// Re-adding is idempotent.
	if out, err := runCLI(t, "allowlist", "add", "acme", "JTI-EXTERNAL"); err != nil || !strings.Contains(out, "already") {
		t.Errorf("allowlist re-add = %q (err %v)", out, err)
	}
	if lst, err := runCLI(t, "allowlist", "list", "acme"); err != nil || !strings.Contains(lst, "JTI-EXTERNAL") {
		t.Errorf("allowlist list = %q (err %v)", lst, err)
	}

	// Remove takes it back out.
	if out, err := runCLI(t, "allowlist", "remove", "acme", "JTI-EXTERNAL", "--yes"); err != nil || !strings.Contains(out, "Removed") {
		t.Errorf("allowlist remove = %q (err %v)", out, err)
	}
	// Removing an absent jti is idempotent success (mirrors add), not an error.
	if out, err := runCLI(t, "allowlist", "remove", "acme", "JTI-EXTERNAL", "--yes"); err != nil || !strings.Contains(out, "not in allowlist") {
		t.Errorf("idempotent remove of absent jti = %q (err %v), want no-op success", out, err)
	}
}

// TestAllowlistExportConsumable asserts the exported file is exactly what a
// server loads: valiss-go's LoadAllowlistFile accepts it and admits the jtis.
func TestAllowlistExportConsumable(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	for _, jti := range []string{"JTI-1", "JTI-2"} {
		if _, err := runCLI(t, "allowlist", "add", "acme", jti); err != nil {
			t.Fatalf("add %s: %v", jti, err)
		}
	}
	out, err := runCLI(t, "allowlist", "export", "acme")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.HasPrefix(out, "#") {
		t.Errorf("export missing header comment:\n%s", out)
	}

	file := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(file, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	al, err := valiss.LoadAllowlistFile(file)
	if err != nil {
		t.Fatalf("LoadAllowlistFile: %v", err)
	}
	for _, jti := range []string{"JTI-1", "JTI-2"} {
		if !al.Allowed(jti) {
			t.Errorf("exported allowlist does not admit %s", jti)
		}
	}
	if al.Allowed("JTI-OTHER") {
		t.Error("exported allowlist admits a jti it should not")
	}
}
