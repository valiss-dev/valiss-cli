package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// runCLI executes the root command with args, capturing stdout. The store
// directory and passphrase are set through the environment so the run is fully
// non-interactive.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// viper is process-global; reset it so a store-dir set in one test does not
	// leak into the next.
	viper.Reset()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestStoreCommandsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "test-passphrase")

	if out, err := runCLI(t, "store", "init", "acme", "--audit-retention", "720h"); err != nil {
		t.Fatalf("store init: %v\n%s", err, out)
	}

	out, err := runCLI(t, "store", "info", "acme")
	if err != nil {
		t.Fatalf("store info: %v\n%s", err, out)
	}
	for _, want := range []string{"acme", "spec 1 / wire 1", "720h"} {
		if !strings.Contains(out, want) {
			t.Errorf("store info missing %q\n%s", want, out)
		}
	}

	if out, err := runCLI(t, "store", "config", "acme", "audit-retention", "168h"); err != nil {
		t.Fatalf("store config set: %v\n%s", err, out)
	}
	out, err = runCLI(t, "store", "config", "acme")
	if err != nil {
		t.Fatalf("store config get: %v\n%s", err, out)
	}
	if !strings.Contains(out, "168h") {
		t.Errorf("store config get did not reflect the set value\n%s", out)
	}

	// A second init of the same operator fails.
	if _, err := runCLI(t, "store", "init", "acme"); err == nil {
		t.Error("second store init succeeded; want failure")
	}
}

func TestStoreInfoMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VALISS_STORE_DIR", dir)
	t.Setenv("VALISS_STORAGE_KEY", "test-passphrase")
	if _, err := runCLI(t, "store", "info", "ghost"); err == nil {
		t.Error("store info on a missing store succeeded; want failure")
	}
}
