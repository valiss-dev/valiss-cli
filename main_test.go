package main

import "testing"

// TestRootCommand is a smoke test: the root command builds, is named after
// the binary, and has its version wired.
func TestRootCommand(t *testing.T) {
	cmd := newRootCommand()

	if cmd.Use != "valiss" {
		t.Errorf("root command Use = %q, want %q", cmd.Use, "valiss")
	}
	if cmd.Version != version {
		t.Errorf("root command Version = %q, want %q", cmd.Version, version)
	}
}
