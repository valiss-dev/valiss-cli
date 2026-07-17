package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// printJSON writes v as indented JSON followed by a newline. It is the shared
// implementation behind every command's --json path, so machine-readable
// output is byte-consistent across the tree (ADR 0021: scriptable by default).
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("valiss: encoding JSON: %w", err)
	}
	return nil
}
