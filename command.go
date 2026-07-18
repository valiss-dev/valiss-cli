package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Entity path depths. Entities are addressed by their position in the
// signing chain, spelled out on the command line with no hidden context
// (ADR 0021): an operator is one segment, an account two, a user three.
const (
	depthOperator = 1
	depthAccount  = 2
	depthUser     = 3
	// A template is a per-operator store object addressed as
	// <operator>/<name>.
	depthTemplate = 2
)

// validatePath checks that s is an entity path of between minDepth and
// maxDepth "/"-separated segments, each segment non-empty.
func validatePath(s string, minDepth, maxDepth int) error {
	if s == "" {
		return errors.New("path must not be empty")
	}
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if p == "" {
			return fmt.Errorf("path %q has an empty segment", s)
		}
	}
	switch n := len(parts); {
	case minDepth == maxDepth && n != minDepth:
		return fmt.Errorf("path %q must name %s", s, depthName(minDepth))
	case n < minDepth || n > maxDepth:
		return fmt.Errorf("path %q must have between %d and %d segments", s, minDepth, maxDepth)
	}
	return nil
}

// depthName renders a segment count as the entity kind it addresses, for
// readable validation errors.
func depthName(depth int) string {
	switch depth {
	case depthOperator:
		return "an operator (<operator>)"
	case depthAccount:
		return "an account (<operator>/<account>)"
	case depthUser:
		return "a user (<operator>/<account>/<user>)"
	default:
		return fmt.Sprintf("%d path segments", depth)
	}
}

// pathArgs builds a positional-argument validator: exactly one entity-path
// argument of depth minDepth..maxDepth, followed by exactly extra further
// non-empty opaque arguments (a jti, a token string, ...).
func pathArgs(minDepth, maxDepth, extra int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		want := 1 + extra
		if len(args) != want {
			return fmt.Errorf("accepts %d arg(s), received %d", want, len(args))
		}
		if err := validatePath(args[0], minDepth, maxDepth); err != nil {
			return err
		}
		for i, a := range args[1:] {
			if a == "" {
				return fmt.Errorf("argument %d must not be empty", i+2)
			}
		}
		return nil
	}
}

// addJSONFlag registers --json on a read command. Scriptable by default:
// list and show emit machine-readable output on request (ADR 0021).
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "emit machine-readable JSON")
}

// addYesFlag registers --yes on a destructive command, skipping the
// confirmation prompt for scripts (ADR 0021).
func addYesFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")
}
