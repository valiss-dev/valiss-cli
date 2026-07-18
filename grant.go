package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"valiss.dev/valiss"
)

// The grant layer turns the token mint command's grant flags, and the grants a
// stamped template carries, into the named extension claims a token embeds
// (ADR 0021). Authorization rides named extension claims: the scheme signs and
// transports them, and a consumer keyed to the same name reads them back.
//
// Two built-in transport grants are emitted in the claim shapes the valiss
// contrib transports consume, so a minted token authorizes against httpauth and
// grpcauth middleware without the CLI importing those packages (the JSON is the
// contract):
//
//   - "http": {"hosts":[...],"methods":[...],"paths":[...]} (httpauth.Ext).
//   - "grpc": {"methods":[...]} (grpcauth.Ext).
//
// Any other extension is a custom, consumer-defined claim carried verbatim
// through the --ext flag (name=payload). "gen" is reserved for the wire-level
// generation extension of ADR 0022.

// reservedExtNames are the extension names --ext may not use: the two built-in
// transport grants and the reserved generation extension.
var reservedExtNames = map[string]bool{"http": true, "grpc": true, "gen": true}

// httpExt is the HTTP transport grant claim, mirroring valiss contrib
// httpauth.Ext. Emitted under the "http" name.
type httpExt struct {
	Hosts   []string `json:"hosts,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

func (httpExt) ExtensionName() string { return "http" }

// grpcExt is the gRPC transport grant claim, mirroring valiss contrib
// grpcauth.Ext. Emitted under the "grpc" name.
type grpcExt struct {
	Methods []string `json:"methods,omitempty"`
}

func (grpcExt) ExtensionName() string { return "grpc" }

// rawExtension is a named extension whose body is opaque JSON supplied
// verbatim through the --ext flag (mint or template). MarshalJSON returns the
// payload byte for byte, so the wire claim is exactly what was provided.
type rawExtension struct {
	name    string
	payload json.RawMessage
}

func (e rawExtension) ExtensionName() string        { return e.name }
func (e rawExtension) MarshalJSON() ([]byte, error) { return e.payload, nil }

// grantBuilder accumulates the grants a mint stamps, from the template first
// and then the explicit flags, deduplicating within each dimension so a grant
// named twice is stamped once. Built-in http/grpc dimensions union; named
// extensions (--ext, from mint or a stamped template) are keyed by name and a
// name set more than once is an error.
type grantBuilder struct {
	httpHosts   stringSet
	httpMethods stringSet
	httpPaths   stringSet
	grpcMethods stringSet
	raw         map[string]json.RawMessage
	rawOrder    []string
}

func newGrantBuilder() *grantBuilder {
	return &grantBuilder{raw: map[string]json.RawMessage{}}
}

// addRaw records a named extension body, rejecting a name set twice.
func (b *grantBuilder) addRaw(name string, payload json.RawMessage) error {
	if _, dup := b.raw[name]; dup {
		return fmt.Errorf("valiss: extension %q is set more than once", name)
	}
	b.raw[name] = payload
	b.rawOrder = append(b.rawOrder, name)
	return nil
}

// addHTTPFlag parses one --http value. A bare value is a host; otherwise the
// value is a ";"-separated list of "dimension=comma,separated,values" clauses,
// with dimension one of hosts, methods, or paths.
func (b *grantBuilder) addHTTPFlag(value string) error {
	if !strings.Contains(value, "=") {
		// A bare value is a host (or comma-separated hosts). Reject a value that is
		// blank, or that carries a clause separator without the dimensioned
		// "key=value" form: both would otherwise mint a garbage host named ";" or
		// silently drop to nothing.
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("valiss: --http value %q is blank", value)
		}
		if strings.Contains(value, ";") {
			return fmt.Errorf("valiss: --http value %q has a ';' clause separator but no key=value dimension; "+
				"use \"hosts=a,b;methods=GET;paths=/v1/*\" to name dimensions, or a bare host without ';'", value)
		}
		b.httpHosts.addAll(splitCSV(value))
		return nil
	}
	for _, clause := range strings.Split(value, ";") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		key, vals, ok := strings.Cut(clause, "=")
		if !ok {
			return fmt.Errorf("valiss: --http clause %q must be key=value", clause)
		}
		items := splitCSV(vals)
		switch strings.TrimSpace(key) {
		case "hosts", "host":
			b.httpHosts.addAll(items)
		case "methods", "method":
			b.httpMethods.addAll(items)
		case "paths", "path":
			b.httpPaths.addAll(items)
		default:
			return fmt.Errorf("valiss: --http key %q is not one of hosts, methods, paths", strings.TrimSpace(key))
		}
	}
	return nil
}

// addGRPCFlag parses one --grpc value: a comma-separated list of full gRPC
// method patterns (a trailing "*" is a prefix wildcard).
func (b *grantBuilder) addGRPCFlag(value string) error {
	b.grpcMethods.addAll(splitCSV(value))
	return nil
}

// addExtFlag parses one --ext value: "name=payload", where payload is inline
// JSON or "@file" naming a file to read. Fail-closed: the payload must be valid
// JSON, the name must not collide with a built-in extension, and a duplicate
// name is rejected.
func (b *grantBuilder) addExtFlag(value string) error {
	name, payload, ok := strings.Cut(value, "=")
	if !ok {
		return fmt.Errorf("valiss: --ext must be name=payload, got %q", value)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("valiss: --ext name must not be empty")
	}
	if reservedExtNames[name] {
		return fmt.Errorf("valiss: --ext name %q is reserved for a built-in extension", name)
	}
	raw, err := loadExtPayload(payload)
	if err != nil {
		return err
	}
	if !json.Valid(raw) {
		return fmt.Errorf("valiss: --ext %q payload is not valid JSON", name)
	}
	return b.addRaw(name, json.RawMessage(raw))
}

// build renders the accumulated grants as extension options for a mint, in a
// deterministic order: http, grpc, then named extensions in the order they
// were added.
func (b *grantBuilder) build() []valiss.IssueOption {
	var opts []valiss.IssueOption
	if b.httpHosts.len()+b.httpMethods.len()+b.httpPaths.len() > 0 {
		opts = append(opts, valiss.WithExtension(httpExt{
			Hosts:   b.httpHosts.sorted(),
			Methods: b.httpMethods.sorted(),
			Paths:   b.httpPaths.sorted(),
		}))
	}
	if b.grpcMethods.len() > 0 {
		opts = append(opts, valiss.WithExtension(grpcExt{Methods: b.grpcMethods.sorted()}))
	}
	for _, name := range b.rawOrder {
		opts = append(opts, valiss.WithExtension(rawExtension{name: name, payload: b.raw[name]}))
	}
	return opts
}

// loadExtPayload resolves a --ext payload: an "@file" reads the file, anything
// else is the inline JSON literal.
func loadExtPayload(p string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(p, "@"); ok {
		data, err := os.ReadFile(rest)
		if err != nil {
			return nil, fmt.Errorf("valiss: reading --ext payload file: %w", err)
		}
		return data, nil
	}
	return []byte(p), nil
}

// splitCSV splits a comma-separated value, trimming spaces and dropping empty
// fields.
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// stringSet is an insertion-tracking, deduplicating string set.
type stringSet struct {
	seen  map[string]struct{}
	items []string
}

func (s *stringSet) add(v string) {
	if v = strings.TrimSpace(v); v == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.items = append(s.items, v)
}

func (s *stringSet) addAll(vs []string) {
	for _, v := range vs {
		s.add(v)
	}
}

func (s *stringSet) len() int { return len(s.items) }

func (s *stringSet) sorted() []string {
	if len(s.items) == 0 {
		return nil
	}
	out := append([]string(nil), s.items...)
	sort.Strings(out)
	return out
}
