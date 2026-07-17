package main

import (
	"strings"
	"testing"
)

func TestTemplateGenerations(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}

	if out, err := runCLI(t, "template", "add", "acme/web", "--http", "api.example.com", "--ttl", "24h"); err != nil {
		t.Fatalf("template add: %v\n%s", err, out)
	}
	// Identical content (grant order should not matter) is a no-op.
	out, err := runCLI(t, "template", "add", "acme/web", "--ttl", "24h", "--http", "api.example.com")
	if err != nil {
		t.Fatalf("template add identical: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("identical add was not a no-op:\n%s", out)
	}
	// New content bumps the generation.
	out, err = runCLI(t, "template", "add", "acme/web", "--http", "api.example.com", "--ttl", "48h")
	if err != nil {
		t.Fatalf("template add gen2: %v\n%s", err, out)
	}
	if !strings.Contains(out, "generation 2") {
		t.Errorf("new content did not bump the generation:\n%s", out)
	}

	// show latest is gen 2; pinned @1 is gen 1.
	out, _ = runCLI(t, "template", "show", "acme/web", "--json")
	if !strings.Contains(out, `"generation": 2`) {
		t.Errorf("show latest not gen 2:\n%s", out)
	}
	out, _ = runCLI(t, "template", "show", "acme/web@1", "--json")
	if !strings.Contains(out, `"generation": 1`) || !strings.Contains(out, "24h") {
		t.Errorf("show @1 not gen 1:\n%s", out)
	}

	// Retire, and the add-a-pin form is rejected.
	if _, err := runCLI(t, "template", "remove", "acme/web", "--yes"); err != nil {
		t.Fatalf("template remove: %v", err)
	}
	out, _ = runCLI(t, "template", "list", "acme")
	if !strings.Contains(out, "retired") {
		t.Errorf("retired template not marked in list:\n%s", out)
	}
	if _, err := runCLI(t, "template", "add", "acme/web@3"); err == nil {
		t.Error("template add with a generation pin succeeded; want rejection")
	}
}

func TestTemplateMissing(t *testing.T) {
	operatorEnv(t)
	if _, err := runCLI(t, "operator", "add", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "template", "show", "acme/ghost"); err == nil {
		t.Error("show of a missing template succeeded; want failure")
	}
	if _, err := runCLI(t, "template", "show", "acme/ghost@2"); err == nil {
		t.Error("show of a missing pinned template succeeded; want failure")
	}
}
