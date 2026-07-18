package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gosqlite "gosqlite.org"
	"gosqlite.org/vfs/crypto"
	"liteorm.org"
	"liteorm.org/dialect/sqlite"
	"liteorm.org/orm"
)

// Local satisfies the backend-agnostic Store surface.
var _ Store = (*Local)(nil)

// saltLen is the KDF salt length. crypto.DeriveKey requires at least
// crypto.MinSaltLen (16) bytes; the salt is unique per store and persisted
// beside the database.
const saltLen = 32

// Local is the filesystem store backend: one encrypted SQLite file per
// operator under a store directory (ADR 0020). Encryption is mandatory
// (Adiantum, always on); there is no plaintext mode.
//
// Judgement call — deferred cksm tamper-evidence. ADR 0020 also calls for the
// gosqlite vfs/cksm page-checksum layer as defense-in-depth. It is not enabled
// here, for two compounding reasons:
//
//   - The cksm VFS's read trampoline performs unsafe pointer arithmetic that
//     trips Go's -d=checkptr instrumentation, which the race detector enables.
//     This repository's CI runs `go test -race ./...`, and with cksm stacked
//     the whole suite aborts with a checkptr fatal error. Encryption alone
//     passes -race cleanly.
//   - gosqlite's own crypto documentation is explicit that crypto+cksm is not
//     authenticated encryption: the Fletcher checksums catch bit-rot, not
//     adversarial tampering, and it points to gosqlite.org/vfs/vault for real
//     integrity. So cksm was always corruption defense-in-depth, never a
//     security boundary.
//
// Mandatory confidentiality — the core ADR 0020 requirement — is therefore
// shipped now; restoring tamper-evidence (via a checkptr-clean cksm release or
// the vault container) is tracked as follow-up. A wrong passphrase is still
// detected: the decrypted pages fail SQLite's own header validation on open.
type Local struct {
	db       *liteorm.DB
	operator string
	dbPath   string
}

// DefaultDir is the default store directory, ~/.valiss/store (ADR 0017, 0020).
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("valiss: locating home directory: %w", err)
	}
	return filepath.Join(home, ".valiss", "store"), nil
}

// dbPath and saltPath name a store's two files: the encrypted database and the
// cleartext KDF salt sidecar.
func dbPath(dir, operator string) string   { return filepath.Join(dir, operator+".db") }
func saltPath(dir, operator string) string { return filepath.Join(dir, operator+".salt") }

// Init creates a new encrypted store for operator under dir and returns it
// open. It fails with ErrExists if the store already exists. The passphrase is
// stretched to a cipher key via Argon2id over a fresh per-store salt; the salt
// is written beside the database (it is not secret, but losing it makes the
// store unrecoverable), and the passphrase itself is never persisted.
//
// The store is created with the audit-retention window in cfg; the operator
// identity itself (keys and the self-signed operator token) is not created
// here — that is the operator verb family's job — so a freshly initialized
// store is a configured, empty container.
func Init(dir, operator string, passphrase []byte, cfg Config) (*Local, error) {
	if _, err := os.Stat(dbPath(dir, operator)); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrExists, dbPath(dir, operator))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("valiss: checking store: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("valiss: creating store directory: %w", err)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("valiss: generating salt: %w", err)
	}
	if err := os.WriteFile(saltPath(dir, operator), salt, 0o600); err != nil {
		return nil, fmt.Errorf("valiss: writing salt: %w", err)
	}

	l, err := openEncrypted(dir, operator, passphrase, salt, true)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if err := orm.AutoMigrateAll(ctx, l.db, schemaModels...); err != nil {
		l.Close()
		return nil, fmt.Errorf("valiss: migrating store: %w", err)
	}
	m := metaRow{
		Operator:              operator,
		CreatedAt:             time.Now().UTC(),
		SpecVersion:           SpecVersion,
		WireVersion:           WireVersion,
		AuditRetentionSeconds: int64(cfg.AuditRetention / time.Second),
	}
	if err := orm.NewRepo[metaRow](l.db).Create(ctx, &m); err != nil {
		l.Close()
		return nil, fmt.Errorf("valiss: writing store metadata: %w", err)
	}
	if err := l.Append(AuditEntry{At: time.Now().UTC(), Op: AuditStoreInit, Path: operator,
		Detail: fmt.Sprintf("audit-retention=%s", cfg.AuditRetention)}); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Open opens an existing store for operator under dir. It fails with
// ErrNotFound if the store does not exist. On open it runs the additive
// migration (forward-compatibility) and the lazy audit-retention sweep; there
// is no daemon (ADR 0021).
func Open(dir, operator string, passphrase []byte) (*Local, error) {
	if _, err := os.Stat(dbPath(dir, operator)); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, dbPath(dir, operator))
	} else if err != nil {
		return nil, fmt.Errorf("valiss: checking store: %w", err)
	}
	salt, err := os.ReadFile(saltPath(dir, operator))
	if err != nil {
		return nil, fmt.Errorf("valiss: reading store salt: %w", err)
	}

	l, err := openEncrypted(dir, operator, passphrase, salt, false)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if err := orm.AutoMigrateAll(ctx, l.db, schemaModels...); err != nil {
		l.Close()
		// A migration failure on open is the usual signature of a wrong
		// passphrase: the decrypted pages are garbage and fail SQLite's header
		// validation. Surface that reading; the raw SQL error is dropped rather
		// than trailing the friendly message with a "file is not a database" tail.
		return nil, ErrUnreadable
	}
	if err := l.sweepAudit(ctx); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// openEncrypted opens the encrypted database with the passphrase stretched to
// an Adiantum key over the per-store salt. See the Local doc comment for why
// the cksm checksum layer is deferred.
func openEncrypted(dir, operator string, passphrase, salt []byte, create bool) (*Local, error) {
	key, err := crypto.DeriveKey(passphrase, salt, crypto.Adiantum)
	if err != nil {
		return nil, fmt.Errorf("valiss: deriving store key: %w", err)
	}
	mode := gosqlite.AccessMode("rw")
	if create {
		mode = "rwc"
	}
	db, err := sqlite.OpenEncryptedConfig(
		gosqlite.Config{Path: dbPath(dir, operator), Mode: mode, Pragmas: gosqlite.RecommendedPragmas()},
		crypto.Options{Key: key, Cipher: crypto.Adiantum},
	)
	if err != nil {
		// Opening an existing store decrypts its pages with the key derived from
		// the supplied passphrase; a wrong passphrase yields garbage that fails
		// SQLite's header check right here, before any query runs. Name the
		// passphrase so the failure is actionable, and drop the raw "file is not
		// a database" sqlite tail from the user-facing message. (Creation cannot
		// hit a wrong passphrase.)
		if !create {
			return nil, ErrUnreadable
		}
		return nil, fmt.Errorf("valiss: opening store: %w", err)
	}
	return &Local{db: db, operator: operator, dbPath: dbPath(dir, operator)}, nil
}

// sweepAudit lazily removes audit entries older than the retention window.
// Zero retention keeps the journal forever (no sweep).
func (l *Local) sweepAudit(ctx context.Context) error {
	cfg, err := l.Config()
	if err != nil {
		return err
	}
	if cfg.AuditRetention <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-cfg.AuditRetention)
	if _, err := l.db.ExecContext(ctx, "DELETE FROM audit WHERE at < ?", cutoff); err != nil {
		return fmt.Errorf("valiss: sweeping audit journal: %w", err)
	}
	return nil
}

// Config returns the store's tunable configuration.
func (l *Local) Config() (Config, error) {
	m, err := l.meta()
	if err != nil {
		return Config{}, err
	}
	return Config{AuditRetention: time.Duration(m.AuditRetentionSeconds) * time.Second}, nil
}

// SetConfig sets one tunable parameter. The only key today is audit-retention,
// a duration.
func (l *Local) SetConfig(key, value string) error {
	m, err := l.meta()
	if err != nil {
		return err
	}
	switch key {
	case "audit-retention":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("valiss: audit-retention must be a duration: %w", err)
		}
		if d < 0 {
			return errors.New("valiss: audit-retention must not be negative")
		}
		m.AuditRetentionSeconds = int64(d / time.Second)
	default:
		return fmt.Errorf("valiss: unknown config key %q", key)
	}
	if err := orm.NewRepo[metaRow](l.db).Update(context.Background(), &m); err != nil {
		return fmt.Errorf("valiss: updating store config: %w", err)
	}
	return l.Append(AuditEntry{At: time.Now().UTC(), Op: AuditStoreConfig, Path: l.operator,
		Detail: fmt.Sprintf("%s=%s", key, value)})
}

// Info reports read-only store facts.
func (l *Local) Info() (Info, error) {
	m, err := l.meta()
	if err != nil {
		return Info{}, err
	}
	ctx := context.Background()
	info := Info{
		Operator:       m.Operator,
		Path:           l.dbPath,
		CreatedAt:      m.CreatedAt,
		SpecVersion:    m.SpecVersion,
		WireVersion:    m.WireVersion,
		AuditRetention: time.Duration(m.AuditRetentionSeconds) * time.Second,
	}
	// Raw row tallies count history (all generations and tombstones, all revoked
	// issuances); the live tallies below reduce them to the current world, so a
	// caller can tell "one live operator" from "six historical rows".
	if info.Entities, err = orm.NewRepo[entityRow](l.db).Count(ctx); err != nil {
		return Info{}, err
	}
	if info.Tokens, err = orm.NewRepo[tokenRow](l.db).Count(ctx); err != nil {
		return Info{}, err
	}
	live, err := l.Subtree(m.Operator)
	if err != nil {
		return Info{}, err
	}
	info.EntitiesLive = int64(len(live))
	liveJTIs, err := l.LiveJTIsUnder(m.Operator)
	if err != nil {
		return Info{}, err
	}
	info.TokensLive = int64(len(liveJTIs))
	if info.Templates, err = orm.NewRepo[templateRow](l.db).Count(ctx); err != nil {
		return Info{}, err
	}
	if info.Allowlist, err = orm.NewRepo[allowlistRow](l.db).Count(ctx); err != nil {
		return Info{}, err
	}
	if info.AuditLines, err = orm.NewRepo[auditRow](l.db).Count(ctx); err != nil {
		return Info{}, err
	}
	if st, err := os.Stat(l.dbPath); err == nil {
		info.SizeBytes = st.Size()
	}
	return info, nil
}

// Append writes one audit-journal entry.
func (l *Local) Append(entry AuditEntry) error {
	row := auditRow{At: entry.At, Op: string(entry.Op), Path: entry.Path, Detail: entry.Detail}
	if row.At.IsZero() {
		row.At = time.Now().UTC()
	}
	if err := orm.NewRepo[auditRow](l.db).Create(context.Background(), &row); err != nil {
		return fmt.Errorf("valiss: appending audit entry: %w", err)
	}
	return nil
}

// Audit reads journal entries under a path subtree, newest first. An empty
// prefix reads the whole journal; a path prefix matches the path itself and
// everything beneath it.
func (l *Local) Audit(pathPrefix string) ([]AuditEntry, error) {
	repo := orm.NewRepo[auditRow](l.db).OrderBy("at DESC")
	if pathPrefix != "" {
		repo = repo.Where("path = ? OR path LIKE ?", pathPrefix, pathPrefix+"/%")
	}
	rows, err := repo.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: reading audit journal: %w", err)
	}
	out := make([]AuditEntry, len(rows))
	for i, r := range rows {
		out[i] = AuditEntry{At: r.At, Op: AuditOp(r.Op), Path: r.Path, Detail: r.Detail}
	}
	return out, nil
}

// Close releases the store, closing the database (which drains the pool and
// tears down the cipher VFS, checkpointing the WAL on a clean close).
func (l *Local) Close() error {
	if l.db != nil {
		return l.db.Close()
	}
	return nil
}

// Operator returns the operator name the store is keyed by.
func (l *Local) Operator() string { return l.operator }

// meta reads the singleton metadata row.
func (l *Local) meta() (metaRow, error) {
	m, err := orm.NewRepo[metaRow](l.db).First(context.Background())
	if err != nil {
		return metaRow{}, fmt.Errorf("valiss: reading store metadata: %w", err)
	}
	return m, nil
}
