package main

import (
	"errors"
	"fmt"
	"time"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"

	"github.com/nats-io/nkeys"
)

// Extension grant claims.
//
// These local types produce the same ext-claim JSON that the library's
// transport extensions do (contrib/httpauth Ext{Hosts,Methods,Paths} under
// "http"; contrib/grpcauth Ext{Methods} under "grpc"), so a server built on
// those packages parses a CLI-minted token unchanged. They are re-declared
// here rather than imported so the CLI does not pull the gRPC and net/http
// transport stacks (and, through grpcauth, all of gRPC and protobuf) into a
// pure issuer-side binary (ADR 0020 keeps the CLI light).
//
// Judgement call — the CLI's grant flags. ADR 0021 names three repeatable
// grant flags (--http, --grpc, --custom) taking "domains" but does not pin
// their claim shape. We map:
//   - --http <host>   -> http grant Hosts (any method, any path on that host)
//   - --grpc <method> -> grpc grant Methods (a full method or a trailing-* prefix)
//   - --custom <name> -> a "custom" grant carrying the given names as Domains
//
// The library defines no "custom" extension, so its shape is a CLI convention:
// a claim named "custom" whose body is {"domains":[...]}, which a consumer that
// registered the name reads back with its own type.

type httpGrant struct {
	Hosts   []string `json:"hosts,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

func (httpGrant) ExtensionName() string { return "http" }

type grpcGrant struct {
	Methods []string `json:"methods,omitempty"`
}

func (grpcGrant) ExtensionName() string { return "grpc" }

type customGrant struct {
	Domains []string `json:"domains,omitempty"`
}

func (customGrant) ExtensionName() string { return "custom" }

// mintRequest is a resolved token-mint request from the command layer.
type mintRequest struct {
	userPath    string
	template    *templateRef
	http        []string
	grpc        []string
	custom      []string
	ttl         time.Duration
	ttlSet      bool
	noExtension bool
	noAllowlist bool
}

// mintToken issues a user token for the addressed user, stamping the union of
// the template's grants (if any) and the explicit grant flags, stores it as an
// issuance, and (unless opted out) deposits its jti in the allowlist. The
// fail-closed extension rule is enforced earlier, in the command's PreRunE.
func mintToken(st *store.Local, req mintRequest) (store.TokenRecord, error) {
	user, err := st.LiveEntity(req.userPath)
	if errors.Is(err, store.ErrNoEntity) {
		return store.TokenRecord{}, fmt.Errorf("valiss: user %q not found; add it with 'valiss user add' first", req.userPath)
	} else if err != nil {
		return store.TokenRecord{}, err
	}
	acct, err := st.LiveEntity(parentOf(req.userPath))
	if err != nil {
		return store.TokenRecord{}, err
	}
	op, err := st.LiveEntity(operatorOf(req.userPath))
	if err != nil {
		return store.TokenRecord{}, err
	}

	// Resolve the template and fold its grants, TTL, and bearer flag together
	// with the explicit flags.
	var (
		http   = req.http
		grpc   = req.grpc
		custom = req.custom
		ttl    = req.ttl
		bearer bool
		tmpl   *store.TemplateRecord
	)
	if req.template != nil {
		rec, err := resolveTemplate(st, *req.template)
		if err != nil {
			return store.TokenRecord{}, err
		}
		if rec.Retired {
			return store.TokenRecord{}, fmt.Errorf("valiss: template %q is retired and cannot be used for new mints", rec.Name)
		}
		tmpl = &rec
		http = unionStrings(parseList(rec.HTTP), http)
		grpc = unionStrings(parseList(rec.GRPC), grpc)
		custom = unionStrings(parseList(rec.Custom), custom)
		bearer = rec.Bearer
		if !req.ttlSet {
			ttl = time.Duration(rec.TTLSeconds) * time.Second
		}
	}

	opts := []valiss.IssueOption{valiss.WithName(user.Name), valiss.WithEpoch(op.Epoch)}
	if ttl > 0 {
		opts = append(opts, valiss.WithTTL(ttl))
	}
	if bearer {
		opts = append(opts, valiss.WithBearer())
	}
	if !req.noExtension {
		if len(http) > 0 {
			opts = append(opts, valiss.WithExtension(httpGrant{Hosts: http}))
		}
		if len(grpc) > 0 {
			opts = append(opts, valiss.WithExtension(grpcGrant{Methods: grpc}))
		}
		if len(custom) > 0 {
			opts = append(opts, valiss.WithExtension(customGrant{Domains: custom}))
		}
	}

	acctKP, err := nkeys.FromSeed(acct.Seed)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: loading account key: %w", err)
	}
	token, err := valiss.IssueUser(acctKP, user.PublicKey, opts...)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: minting token: %w", err)
	}
	claims, err := valiss.Decode(token)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: decoding minted token: %w", err)
	}

	rec := store.TokenRecord{
		JTI:      claims.ID,
		Subject:  req.userPath,
		Level:    store.KindUser,
		Token:    token,
		MintedAt: time.Now().UTC(),
	}
	if !claims.ExpiresAt.IsZero() {
		rec.ExpiresAt = claims.ExpiresAt.UTC()
	}
	if tmpl != nil {
		rec.TemplateName = tmpl.Name
		rec.TemplateGen = tmpl.Generation
		rec.TemplateHash = tmpl.ContentHash
	}
	if err := st.PutToken(rec); err != nil {
		return store.TokenRecord{}, err
	}
	if !req.noAllowlist {
		if err := st.AddAllowlist(rec.JTI); err != nil {
			return store.TokenRecord{}, err
		}
	}
	detail := fmt.Sprintf("user token %s", rec.JTI)
	if tmpl != nil {
		detail += fmt.Sprintf(" template=%s@%d", tmpl.Name, tmpl.Generation)
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTokenMint, Path: req.userPath, Detail: detail}); err != nil {
		return store.TokenRecord{}, err
	}
	return rec, nil
}

// unionStrings returns the union of two string lists, preserving first-seen
// order and dropping duplicates.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, v := range append(append([]string(nil), a...), b...) {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
