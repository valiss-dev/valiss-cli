// Package store is the valiss CLI's credential store: one encrypted SQLite
// file per operator, holding the operator/account/user signing chain, the
// claim templates, the issued tokens, the local jti allowlist, and the
// append-only audit journal (ADR 0020, ADR 0021).
//
// The package is backend-agnostic at its surface: callers hold a Store, and
// the local gosqlite/liteorm implementation (Local) is the first and only
// backend today. The schema is generation- and tombstone-aware from the first
// migration, as ADR 0021 requires, so history and rotation have a place to
// live even before every verb that writes them exists.
//
// The store deliberately does not import the cobra/viper stack: it is pure
// store logic, wired to the command tree from the main package.
package store

import (
	"errors"
	"time"
)

// Entity kinds, the three store-entity levels of the signing chain.
const (
	KindOperator = "operator"
	KindAccount  = "account"
	KindUser     = "user"
)

// Wire and spec constants recorded in a store's metadata. They pin the format
// a store was written against; a future format bump is a migration.
const (
	// SpecVersion is the wire specification a store's tokens conform to
	// (SPEC-1). Distinct from the wire-format version and from any library or
	// generation counter (ADR 0021, ADR 0006).
	SpecVersion = 1
	// WireVersion is the token wire-format version stores mint at (ADR 0009).
	WireVersion = 1
)

// Common store errors.
var (
	// ErrExists is returned when initializing a store that already exists.
	ErrExists = errors.New("valiss: store already exists")
	// ErrNotFound is returned when opening a store that does not exist.
	ErrNotFound = errors.New("valiss: store not found")
	// ErrNoEntity is returned when an addressed entity is absent.
	ErrNoEntity = errors.New("valiss: entity not found")
)

// Config holds the tunable, store-global parameters (ADR 0021). Today the only
// tunable is the audit-journal retention window.
type Config struct {
	// AuditRetention is how long audit-journal entries are kept before the
	// lazy sweep on store open removes them. Zero keeps them forever.
	AuditRetention time.Duration
}

// Info is the read-only fact sheet store info reports.
type Info struct {
	// Operator is the operator name the store is keyed by (its file name).
	Operator string
	// Path is the store file's location on disk.
	Path string
	// CreatedAt is when the store was initialized.
	CreatedAt time.Time
	// SpecVersion and WireVersion pin the formats the store was written
	// against.
	SpecVersion int
	WireVersion int
	// AuditRetention is the current retention window (0 = forever).
	AuditRetention time.Duration
	// Counts are live entity/token/template/allowlist tallies.
	Entities   int64
	Tokens     int64
	Templates  int64
	Allowlist  int64
	AuditLines int64
	// SizeBytes is the store file's size on disk.
	SizeBytes int64
}

// AuditOp is an audit-journal operation code.
type AuditOp string

// Audit operation codes, one per journaled action (ADR 0021).
const (
	AuditStoreInit       AuditOp = "store.init"
	AuditStoreConfig     AuditOp = "store.config"
	AuditEntityAdd       AuditOp = "entity.add"
	AuditEntityRemove    AuditOp = "entity.remove"
	AuditOperatorRotate  AuditOp = "operator.rotate"
	AuditTokenMint       AuditOp = "token.mint"
	AuditTokenRevoke     AuditOp = "token.revoke"
	AuditTemplateAdd     AuditOp = "template.add"
	AuditTemplateRetire  AuditOp = "template.retire"
	AuditCredsExport     AuditOp = "creds.export"
	AuditAllowlistAdd    AuditOp = "allowlist.add"
	AuditAllowlistRemove AuditOp = "allowlist.remove"
	AuditAllowlistExport AuditOp = "allowlist.export"
)

// AuditEntry is one append-only journal line: a timestamped record of an
// operation against an entity path, with free-form detail. Entries are never
// updated or deleted except by the retention sweep.
type AuditEntry struct {
	At     time.Time
	Op     AuditOp
	Path   string
	Detail string
}

// Store is the credential store surface the command tree drives. The local
// gosqlite/liteorm backend is the only implementation today; a remote or
// keychain-backed backend would satisfy the same interface (ADR 0020).
//
// The surface grows verb-family by verb-family as the command bodies land;
// this foundation carries store lifecycle, configuration, and the audit
// journal, which every later verb writes to.
type Store interface {
	// Info reports read-only store facts.
	Info() (Info, error)
	// Config returns the current tunable configuration.
	Config() (Config, error)
	// SetConfig sets one tunable parameter by key and string value, validating
	// the key and value.
	SetConfig(key, value string) error
	// Append writes one audit-journal entry.
	Append(entry AuditEntry) error
	// Audit reads journal entries under a path subtree, newest first.
	Audit(pathPrefix string) ([]AuditEntry, error)
	// Close releases the store, checkpointing the WAL on a clean close.
	Close() error
}
