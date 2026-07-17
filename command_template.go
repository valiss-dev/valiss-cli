package main

import "github.com/spf13/cobra"

// newTemplateCommand builds the template noun. Templates are per-operator
// store objects holding claimsets only (grants, TTL, the bearer flag, a
// description), never identity claims. They carry generations: a fresh add
// under an existing name creates the next generation (ADR 0021). A template
// is addressed as <operator>/<name>; show and audit accept a <name>@<N>
// generation pin.
func newTemplateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage claim templates",
		Long:  "Manage per-operator claim templates: repeatable claimsets stamped into issuances at mint time.",
	}

	add := &cobra.Command{
		Use:   "add <operator>/<name>",
		Short: "Add a template generation",
		Long: "Add a template. Re-adding under an existing name with new " +
			"content creates the next generation of that name.",
		Args: pathArgs(depthTemplate, depthTemplate, 0),
		RunE: stub,
	}
	add.Flags().StringSlice("http", nil, "grant an HTTP extension for the given domain (repeatable)")
	add.Flags().StringSlice("grpc", nil, "grant a gRPC extension for the given domain (repeatable)")
	add.Flags().StringSlice("custom", nil, "grant a custom-scheme extension for the given domain (repeatable)")
	add.Flags().Duration("ttl", 0, "token time-to-live carried by the template")
	add.Flags().Bool("bearer", false, "mark issued tokens as bearer tokens")
	add.Flags().String("description", "", "human-readable description of the template")

	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List templates under an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  stub,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<name>[@<gen>]",
		Short: "Show a template",
		Long:  "Show a template. Without a generation pin the latest generation is shown.",
		Args:  pathArgs(depthTemplate, depthTemplate, 0),
		RunE:  stub,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<name>",
		Short: "Retire a template",
		Long: "Retire a template name for new mints. Its generations garbage " +
			"collect once no retained issuance still references them.",
		Args: pathArgs(depthTemplate, depthTemplate, 0),
		RunE: stub,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<name>",
		Short: "Read the template audit journal",
		Long:  "Read the template's generation history and the mints that reference it.",
		Args:  pathArgs(depthTemplate, depthTemplate, 0),
		RunE:  stub,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}
