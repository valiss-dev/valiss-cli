package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"valiss.dev/cli/valiss/internal/store"
)

// templateContent is the claim material a template carries: extension grant
// domains, a TTL, the bearer flag, and a description. It never carries identity
// claims (ADR 0021).
type templateContent struct {
	HTTP        []string      `json:"http,omitempty"`
	GRPC        []string      `json:"grpc,omitempty"`
	Custom      []string      `json:"custom,omitempty"`
	TTL         time.Duration `json:"ttl,omitempty"`
	Bearer      bool          `json:"bearer,omitempty"`
	Description string        `json:"description,omitempty"`
}

// canonicalHash is the content hash stamped into issuance records so an audit
// reads correctly after a template evolves (ADR 0021). It is computed over a
// stable JSON encoding with sorted grant lists, so equal content always hashes
// equal regardless of flag order.
func (c templateContent) canonicalHash() string {
	cp := c
	cp.HTTP = sortedCopy(c.HTTP)
	cp.GRPC = sortedCopy(c.GRPC)
	cp.Custom = sortedCopy(c.Custom)
	// The description is claim material a template carries, but it is not an
	// authorization grant; it is included in the hash so a description-only
	// edit is still a new generation, keeping the audit honest.
	b, _ := json.Marshal(cp)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// templateSummary is the display view of a template generation.
type templateSummary struct {
	Name        string   `json:"name"`
	Generation  uint64   `json:"generation"`
	HTTP        []string `json:"http,omitempty"`
	GRPC        []string `json:"grpc,omitempty"`
	Custom      []string `json:"custom,omitempty"`
	TTL         string   `json:"ttl,omitempty"`
	Bearer      bool     `json:"bearer,omitempty"`
	Description string   `json:"description,omitempty"`
	ContentHash string   `json:"content_hash"`
	Retired     bool     `json:"retired,omitempty"`
	Created     string   `json:"created,omitempty"`
}

// summarizeTemplate builds a display summary from a persisted template.
func summarizeTemplate(r store.TemplateRecord) templateSummary {
	s := templateSummary{
		Name:        r.Name,
		Generation:  r.Generation,
		HTTP:        parseList(r.HTTP),
		GRPC:        parseList(r.GRPC),
		Custom:      parseList(r.Custom),
		Bearer:      r.Bearer,
		Description: r.Description,
		ContentHash: r.ContentHash,
		Retired:     r.Retired,
	}
	if r.TTLSeconds > 0 {
		s.TTL = (time.Duration(r.TTLSeconds) * time.Second).String()
	}
	if !r.CreatedAt.IsZero() {
		s.Created = r.CreatedAt.UTC().Format(time.RFC3339)
	}
	return s
}

// addTemplate records a template generation. If the name is new, it starts at
// generation 1; if it exists and the content differs from the latest
// generation, it creates the next generation; if the content is identical to
// the latest, it is a no-op that returns the existing record (created=false).
func addTemplate(st *store.Local, name string, content templateContent) (rec store.TemplateRecord, created bool, err error) {
	hash := content.canonicalHash()
	var generation uint64 = 1
	latest, err := st.LatestTemplate(name)
	switch {
	case err == nil:
		if latest.ContentHash == hash {
			return latest, false, nil
		}
		generation = latest.Generation + 1
	case errors.Is(err, store.ErrNoTemplate):
		// first generation
	default:
		return store.TemplateRecord{}, false, err
	}

	salt, err := randomSalt()
	if err != nil {
		return store.TemplateRecord{}, false, err
	}
	rec = store.TemplateRecord{
		Name:        name,
		Generation:  generation,
		HTTP:        marshalList(content.HTTP),
		GRPC:        marshalList(content.GRPC),
		Custom:      marshalList(content.Custom),
		TTLSeconds:  int64(content.TTL / time.Second),
		Bearer:      content.Bearer,
		Description: content.Description,
		Salt:        salt,
		ContentHash: hash,
		CreatedAt:   time.Now().UTC(),
	}
	if err := st.PutTemplate(rec); err != nil {
		return store.TemplateRecord{}, false, err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTemplateAdd, Path: name,
		Detail: fmt.Sprintf("generation %d hash=%s", generation, shortHash(hash))}); err != nil {
		return store.TemplateRecord{}, false, err
	}
	return rec, true, nil
}

// templateRef is a parsed template address: the operator, the template name,
// and an optional pinned generation.
type templateRef struct {
	operator   string
	name       string
	generation uint64
	pinned     bool
}

// parseTemplateRef parses an "<operator>/<name>[@<gen>]" argument. A trailing
// "@<gen>" pins a generation exactly (a bare numeric pin, no "v" prefix).
func parseTemplateRef(arg string) (templateRef, error) {
	operator := operatorOf(arg)
	nameGen := childName(arg)
	name, genStr, pinned := strings.Cut(nameGen, "@")
	if name == "" {
		return templateRef{}, fmt.Errorf("valiss: template name must not be empty in %q", arg)
	}
	ref := templateRef{operator: operator, name: name}
	if pinned {
		gen, err := strconv.ParseUint(genStr, 10, 64)
		if err != nil || gen == 0 {
			return templateRef{}, fmt.Errorf("valiss: template generation pin must be a positive integer, got %q", genStr)
		}
		ref.generation = gen
		ref.pinned = true
	}
	return ref, nil
}

// parseBareTemplateRef parses a token mint "--template <name>[@<gen>]" value.
// Unlike parseTemplateRef it carries no operator segment: the operator comes
// from the minted entity's path. A trailing "@<gen>" pins a generation exactly.
func parseBareTemplateRef(s string) (templateRef, error) {
	name, genStr, pinned := strings.Cut(s, "@")
	if name == "" {
		return templateRef{}, fmt.Errorf("valiss: template name must not be empty in %q", s)
	}
	ref := templateRef{name: name}
	if pinned {
		gen, err := strconv.ParseUint(genStr, 10, 64)
		if err != nil || gen == 0 {
			return templateRef{}, fmt.Errorf("valiss: template generation pin must be a positive integer, got %q", genStr)
		}
		ref.generation = gen
		ref.pinned = true
	}
	return ref, nil
}

// resolveTemplate loads the referenced template generation: the pinned one, or
// the latest.
func resolveTemplate(st *store.Local, ref templateRef) (store.TemplateRecord, error) {
	if ref.pinned {
		return st.TemplateAt(ref.name, ref.generation)
	}
	return st.LatestTemplate(ref.name)
}

// randomSalt generates the short random salt each template record carries for
// the ADR 0022 wire-level name-digest scheme. Cross-template digest-collision
// detection (ADR 0022) is deferred with the wire reflection it serves; the salt
// is recorded now so the schema is ready.
func randomSalt() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("valiss: generating template salt: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// marshalList JSON-encodes a string list, returning "" for an empty list.
func marshalList(v []string) string {
	if len(v) == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// parseList decodes a JSON string list, returning nil for "".
func parseList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// sortedCopy returns a sorted copy of a string slice, leaving the input
// untouched.
func sortedCopy(v []string) []string {
	if len(v) == 0 {
		return v
	}
	cp := append([]string(nil), v...)
	sort.Strings(cp)
	return cp
}

// shortHash renders the leading bytes of a content hash for audit detail.
func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
