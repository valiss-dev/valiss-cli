package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"valiss.dev/cli/valiss/internal/store"
)

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
			"content creates the next generation of that name; re-adding " +
			"identical content is a no-op. The grant flags mirror token mint " +
			"(--http/--grpc/--ext), so a template expresses the same grant shapes " +
			"a direct mint can; a stamped template's grants union with a mint's " +
			"own grant flags.",
		Example: "  # Generation 1\n" +
			"  valiss template add acme/web --http \"hosts=api.example.com;methods=GET,POST\" --ttl 24h\n\n" +
			"  # A new grant under the same name creates generation 2\n" +
			"  valiss template add acme/web --http \"hosts=api.example.com;methods=GET,POST,DELETE\" --ttl 24h",
		Args: pathArgs(depthTemplate, depthTemplate, 0),
		RunE: runTemplateAdd,
	}
	add.Flags().StringArray("http", nil, "grant an HTTP extension (repeatable); a bare value is a host, or "+
		"\"hosts=a,b;methods=GET;paths=/v1/*\" to name dimensions")
	add.Flags().StringArray("grpc", nil, "grant a gRPC extension for the given full method(s) (repeatable, comma-separated)")
	add.Flags().StringArray("ext", nil, "grant a custom extension as <name>=<json> or <name>=@<file> (repeatable)")
	add.Flags().Duration("ttl", 0, "token time-to-live carried by the template (0 = no expiry)")
	add.Flags().Bool("bearer", false, "mark issued tokens as bearer tokens")
	add.Flags().String("description", "", "human-readable description of the template")

	list := &cobra.Command{
		Use:   "list <operator>",
		Short: "List templates under an operator",
		Args:  pathArgs(depthOperator, depthOperator, 0),
		RunE:  runTemplateList,
	}
	addJSONFlag(list)

	show := &cobra.Command{
		Use:   "show <operator>/<name>[@<gen>]",
		Short: "Show a template",
		Long:  "Show a template. Without a generation pin the latest generation is shown; a trailing @<gen> pins an exact generation.",
		Example: "  # The latest generation\n" +
			"  valiss template show acme/web\n\n" +
			"  # Pin an exact generation\n" +
			"  valiss template show acme/web@1",
		Args: pathArgs(depthTemplate, depthTemplate, 0),
		RunE: runTemplateShow,
	}
	addJSONFlag(show)

	remove := &cobra.Command{
		Use:   "remove <operator>/<name>",
		Short: "Retire a template",
		Long: "Retire a template name for new mints. Its generations garbage " +
			"collect once no retained issuance still references them.",
		Args: pathArgs(depthTemplate, depthTemplate, 0),
		RunE: runTemplateRemove,
	}
	addYesFlag(remove)

	audit := &cobra.Command{
		Use:   "audit <operator>/<name>",
		Short: "Read the template audit journal",
		Long:  "Read the template's generation history and the mints that reference it.",
		Args:  pathArgs(depthTemplate, depthTemplate, 0),
		RunE:  runTemplateAudit,
	}
	addJSONFlag(audit)

	cmd.AddCommand(add, list, show, remove, audit)
	return cmd
}

func runTemplateAdd(cmd *cobra.Command, args []string) error {
	operator := operatorOf(args[0])
	name := childName(args[0])
	if strings.Contains(name, "@") {
		return fmt.Errorf("valiss: template add does not take a generation pin; got %q", name)
	}
	content, err := templateContentFromFlags(cmd)
	if err != nil {
		return err
	}

	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	rec, created, err := addTemplate(st, name, content)
	if err != nil {
		return err
	}
	if !created {
		fmt.Fprintf(cmd.OutOrStdout(), "Template %q unchanged (identical to generation %d)\n", name, rec.Generation)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added template %q generation %d\n", name, rec.Generation)
	return nil
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	st, err := openStore(args[0])
	if err != nil {
		return err
	}
	defer st.Close()

	recs, err := st.ListTemplates()
	if err != nil {
		return err
	}
	summaries := make([]templateSummary, 0, len(recs))
	for _, r := range recs {
		summaries = append(summaries, summarizeTemplate(r))
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), summaries)
	}
	w := cmd.OutOrStdout()
	if len(summaries) == 0 {
		fmt.Fprintln(w, "none")
		return nil
	}
	for _, s := range summaries {
		retired := ""
		if s.Retired {
			retired = " (retired)"
		}
		fmt.Fprintf(w, "%-24s gen=%d  ttl=%s bearer=%t%s\n", s.Name, s.Generation, dashIfEmpty(s.TTL), s.Bearer, retired)
	}
	return nil
}

func runTemplateShow(cmd *cobra.Command, args []string) error {
	ref, err := parseTemplateRef(args[0])
	if err != nil {
		return err
	}
	st, err := openStore(ref.operator)
	if err != nil {
		return err
	}
	defer st.Close()

	rec, err := resolveTemplate(st, ref)
	if errors.Is(err, store.ErrNoTemplate) {
		return err
	}
	if err != nil {
		return err
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	s := summarizeTemplate(rec)
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), s)
	}
	return writeTemplate(cmd, s)
}

func runTemplateRemove(cmd *cobra.Command, args []string) error {
	operator := operatorOf(args[0])
	name := childName(args[0])
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	if _, err := st.LatestTemplate(name); err != nil {
		return err
	}
	ok, err := confirmed(cmd, fmt.Sprintf("Retire template %q for new mints?", name))
	if err != nil || !ok {
		return err
	}
	if err := st.RetireTemplate(name); err != nil {
		return err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTemplateRetire, Path: name, Detail: "retired for new mints"}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Retired template %q\n", name)
	return nil
}

func runTemplateAudit(cmd *cobra.Command, args []string) error {
	operator := operatorOf(args[0])
	name := childName(args[0])
	st, err := openStore(operator)
	if err != nil {
		return err
	}
	defer st.Close()

	gens, err := st.TemplateGenerations(name)
	if err != nil {
		return err
	}
	if len(gens) == 0 {
		return fmt.Errorf("%w: %s", store.ErrNoTemplate, name)
	}
	summaries := make([]templateSummary, len(gens))
	for i, g := range gens {
		summaries[i] = summarizeTemplate(g)
	}
	// The referencing mints: the issuances that stamped this template. ADR 0021
	// wants the audit to show both the generation history and the mints that
	// reference it, joined by template name/generation.
	mints, err := st.TokensForTemplate(name)
	if err != nil {
		return err
	}
	refs := make([]templateMintRef, len(mints))
	for i, m := range mints {
		refs[i] = templateMintRef{
			JTI:        m.JTI,
			Generation: m.TemplateGen,
			Subject:    m.Subject,
			Status:     tokenStatus(m),
		}
		if !m.MintedAt.IsZero() {
			refs[i].Minted = m.MintedAt.UTC().Format(time.RFC3339)
		}
	}
	jsonOut, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(cmd.OutOrStdout(), templateAuditJSON{Generations: summaries, Mints: refs})
	}
	w := cmd.OutOrStdout()
	for _, s := range summaries {
		retired := ""
		if s.Retired {
			retired = " retired"
		}
		fmt.Fprintf(w, "gen %-3d %s  hash=%s%s\n", s.Generation, dashIfEmpty(s.Created), shortHash(s.ContentHash), retired)
	}
	if len(refs) == 0 {
		fmt.Fprintln(w, "mints: none")
		return nil
	}
	fmt.Fprintln(w, "mints:")
	for _, r := range refs {
		fmt.Fprintf(w, "  %s  gen %-3d %-7s %s\n", r.JTI, r.Generation, r.Status, r.Subject)
	}
	return nil
}

// templateMintRef is one issuance that stamped a template, for the audit's
// referencing-mints view.
type templateMintRef struct {
	JTI        string `json:"jti"`
	Generation uint64 `json:"generation"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	Minted     string `json:"minted,omitempty"`
}

// templateAuditJSON is the machine-readable template audit: the generation
// history and the mints that reference the template.
type templateAuditJSON struct {
	Generations []templateSummary `json:"generations"`
	Mints       []templateMintRef `json:"mints"`
}

// templateContentFromFlags reads the claimset flags into a templateContent.
func templateContentFromFlags(cmd *cobra.Command) (templateContent, error) {
	http, err := cmd.Flags().GetStringArray("http")
	if err != nil {
		return templateContent{}, err
	}
	grpc, err := cmd.Flags().GetStringArray("grpc")
	if err != nil {
		return templateContent{}, err
	}
	ext, err := cmd.Flags().GetStringArray("ext")
	if err != nil {
		return templateContent{}, err
	}
	ttl, err := cmd.Flags().GetDuration("ttl")
	if err != nil {
		return templateContent{}, err
	}
	if ttl < 0 {
		return templateContent{}, fmt.Errorf("valiss: --ttl must not be negative (got %s); use 0 for no expiry", ttl)
	}
	bearer, err := cmd.Flags().GetBool("bearer")
	if err != nil {
		return templateContent{}, err
	}
	description, err := cmd.Flags().GetString("description")
	if err != nil {
		return templateContent{}, err
	}
	return templateContent{HTTP: http, GRPC: grpc, Ext: ext, TTL: ttl, Bearer: bearer, Description: description}, nil
}

// writeTemplate renders one template generation as text.
func writeTemplate(cmd *cobra.Command, s templateSummary) error {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-14s %s\n", "name:", s.Name)
	fmt.Fprintf(w, "%-14s %d\n", "generation:", s.Generation)
	if len(s.HTTP) > 0 {
		fmt.Fprintf(w, "%-14s %s\n", "http:", strings.Join(s.HTTP, ", "))
	}
	if len(s.GRPC) > 0 {
		fmt.Fprintf(w, "%-14s %s\n", "grpc:", strings.Join(s.GRPC, ", "))
	}
	if len(s.Ext) > 0 {
		fmt.Fprintf(w, "%-14s %s\n", "ext:", strings.Join(s.Ext, ", "))
	}
	if s.TTL != "" {
		fmt.Fprintf(w, "%-14s %s\n", "ttl:", s.TTL)
	}
	fmt.Fprintf(w, "%-14s %t\n", "bearer:", s.Bearer)
	if s.Description != "" {
		fmt.Fprintf(w, "%-14s %s\n", "description:", s.Description)
	}
	fmt.Fprintf(w, "%-14s %s\n", "content hash:", s.ContentHash)
	if s.Retired {
		fmt.Fprintf(w, "%-14s %t\n", "retired:", s.Retired)
	}
	if s.Created != "" {
		fmt.Fprintf(w, "%-14s %s\n", "created:", s.Created)
	}
	return nil
}

// dashIfEmpty renders "-" for an empty string, for aligned columns.
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
