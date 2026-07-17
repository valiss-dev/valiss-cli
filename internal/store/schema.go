package store

import "time"

// The store schema. Every model is generation- and tombstone-aware from this
// first migration, as ADR 0021 requires: the schema has to model generations
// and history from day one so rotation, removal, and the wire-level generation
// reflection of ADR 0022 are additive later rather than schema breaks. Bodies
// that write most of these tables land with their verb families in later PRs;
// the tables exist now so those PRs add only logic, not migrations.
//
// AutoMigrate is additive (CREATE TABLE / ADD COLUMN only), so introducing a
// column later is safe; renaming or dropping one is a deliberate migration.

// metaRow is the single-row store metadata: the operator the store is keyed
// by, when it was created, the formats it was written against, and the tunable
// audit-retention window.
type metaRow struct {
	ID                    int64
	Operator              string    `orm:"operator,notnull"`
	CreatedAt             time.Time `orm:"created_at,notnull"`
	SpecVersion           int       `orm:"spec_version,notnull"`
	WireVersion           int       `orm:"wire_version,notnull"`
	AuditRetentionSeconds int64     `orm:"audit_retention_seconds,notnull"`
}

func (metaRow) TableName() string { return "meta" }

// entityRow is one generation of a store entity (operator, account, or user).
// The store is append-only: a new generation is a new row, and a removal is a
// tombstone row, so the table is the entity's whole history. The live view of
// a path is its highest-generation, non-tombstone row.
type entityRow struct {
	ID int64
	// Kind is operator, account, or user.
	Kind string `orm:"kind,notnull"`
	// Path is the entity's position in the chain: "op", "op/acct", or
	// "op/acct/user".
	Path string `orm:"path,notnull,index"`
	// Parent is the parent path ("" for the operator), so a subtree query is a
	// prefix scan.
	Parent string `orm:"parent"`
	// Name is the human-readable label carried into the token's name claim.
	Name string `orm:"name"`
	// PublicKey is the entity's nkey public key.
	PublicKey string `orm:"public_key,notnull"`
	// Seed is the entity's nkey seed, the private key. It is encrypted at rest
	// by the store's page-level VFS (ADR 0020); it never leaves the store in
	// the clear except through a deliberate creds export.
	Seed []byte `orm:"seed"`
	// Generation is the entity's lifecycle counter (ADR 0021). It bumps on an
	// invalidating change (rotation, removal, an extension-policy change).
	Generation uint64 `orm:"generation,notnull"`
	// Epoch is the trust-domain epoch stamped on tokens minted under this
	// entity. For an operator it is the domain epoch; account and user tokens
	// echo the operator's current epoch (see the reconciliation note in the
	// operator verb family).
	Epoch uint64 `orm:"epoch,notnull"`
	// Token is the entity's current self- or parent-signed token.
	Token string `orm:"token"`
	// CreatedAt is when this generation was written.
	CreatedAt time.Time `orm:"created_at,notnull"`
	// Tombstone marks a removal row: the generation at which the entity left
	// the world.
	Tombstone bool `orm:"tombstone,notnull"`
}

func (entityRow) TableName() string { return "entities" }

// tokenRow is one issuance: a minted token identified by its jti. Tokens are
// revoked (jti leaves the allowlist), never removed, so a revoked token keeps
// its row with the revocation stamped.
type tokenRow struct {
	ID int64
	// JTI is the token id, the allowlist key.
	JTI string `orm:"jti,notnull,index"`
	// Subject is the entity path the token was minted for.
	Subject string `orm:"subject,notnull,index"`
	// Level is the token level: account or user (message tokens are not stored
	// issuances).
	Level string `orm:"level,notnull"`
	// Token is the JWT.
	Token string `orm:"token,notnull"`
	// TemplateName, TemplateGen, and TemplateHash stamp the template a mint
	// used, so an audit reads correctly even after the template evolves
	// (ADR 0021). Empty when no template was used.
	TemplateName string `orm:"template_name"`
	TemplateGen  uint64 `orm:"template_gen"`
	TemplateHash string `orm:"template_hash"`
	// MintedAt is the issuance time; ExpiresAt is the token expiry (zero =
	// never).
	MintedAt  time.Time `orm:"minted_at,notnull"`
	ExpiresAt time.Time `orm:"expires_at"`
	// Revoked and RevokedAt record a revocation.
	Revoked   bool      `orm:"revoked,notnull"`
	RevokedAt time.Time `orm:"revoked_at"`
}

func (tokenRow) TableName() string { return "tokens" }

// templateRow is one generation of a per-operator claim template. Templates
// hold claim material only (extension grants, TTL, the bearer flag, a
// description), never identity claims. A fresh add under an existing name with
// new content is the next generation; generations are retained as long as a
// retained issuance still references them (ADR 0021).
type templateRow struct {
	ID int64
	// Name is the template name; Generation is its lifecycle counter.
	Name       string `orm:"name,notnull,index"`
	Generation uint64 `orm:"generation,notnull"`
	// HTTP, GRPC, and Custom are the extension grant domains, JSON-encoded
	// string lists.
	HTTP   string `orm:"http"`
	GRPC   string `orm:"grpc"`
	Custom string `orm:"custom"`
	// TTLSeconds is the template's token TTL (0 = none); Bearer marks issued
	// tokens as bearer; Description is free text.
	TTLSeconds  int64  `orm:"ttl_seconds"`
	Bearer      bool   `orm:"bearer,notnull"`
	Description string `orm:"description"`
	// Salt is the short random salt for the ADR 0022 wire-level name-digest
	// scheme; ContentHash stamps the generation's content into issuance
	// records.
	Salt        string `orm:"salt,notnull"`
	ContentHash string `orm:"content_hash,notnull"`
	// CreatedAt is when this generation was written; Retired/RetiredAt record a
	// retirement (the name stops accepting new mints).
	CreatedAt time.Time `orm:"created_at,notnull"`
	Retired   bool      `orm:"retired,notnull"`
	RetiredAt time.Time `orm:"retired_at"`
}

func (templateRow) TableName() string { return "templates" }

// allowlistRow is one jti in the local allowlist. The allowlist is the
// fail-closed revocation surface; export renders it in the newline-delimited
// form servers consume (ADR 0021).
type allowlistRow struct {
	ID      int64
	JTI     string    `orm:"jti,notnull,unique"`
	AddedAt time.Time `orm:"added_at,notnull"`
}

func (allowlistRow) TableName() string { return "allowlist" }

// auditRow is one append-only journal line. Entries are never updated and are
// removed only by the lazy retention sweep on store open (ADR 0021).
type auditRow struct {
	ID     int64
	At     time.Time `orm:"at,notnull,index"`
	Op     string    `orm:"op,notnull"`
	Path   string    `orm:"path,index"`
	Detail string    `orm:"detail"`
}

func (auditRow) TableName() string { return "audit" }

// schemaModels is the full model set AutoMigrate creates. Keeping it in one
// place means the first migration establishes every table, generations and
// tombstones included.
var schemaModels = []any{
	metaRow{}, entityRow{}, tokenRow{}, templateRow{}, allowlistRow{}, auditRow{},
}
