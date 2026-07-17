package main

import (
	"strings"
	"testing"
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
