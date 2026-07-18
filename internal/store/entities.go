package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"liteorm.org"
	"liteorm.org/orm"
)

// EntityRecord is one persisted generation of a store entity. It is the data
// the store holds; key generation and token minting live above the store, in
// the chain-orchestration layer, so this package stays free of the crypto
// stack.
type EntityRecord struct {
	Kind       string
	Path       string
	Parent     string
	Name       string
	PublicKey  string
	Seed       []byte
	Generation uint64
	Epoch      uint64
	Token      string
	CreatedAt  time.Time
	Tombstone  bool
}

// PutEntity appends an entity generation row. The store is append-only: each
// call writes a new row, never updating an existing one, so the table is the
// entity's whole history.
func (l *Local) PutEntity(r EntityRecord) error {
	row := entityRow{
		Kind:       r.Kind,
		Path:       r.Path,
		Parent:     r.Parent,
		Name:       r.Name,
		PublicKey:  r.PublicKey,
		Seed:       r.Seed,
		Generation: r.Generation,
		Epoch:      r.Epoch,
		Token:      r.Token,
		CreatedAt:  nonZeroTime(r.CreatedAt),
		Tombstone:  r.Tombstone,
	}
	if err := orm.NewRepo[entityRow](l.db).Create(context.Background(), &row); err != nil {
		return fmt.Errorf("valiss: writing entity %q: %w", r.Path, err)
	}
	return nil
}

// LiveEntity returns the live view of a path: its highest-generation row, or
// ErrNoEntity when the path has no rows or its latest generation is a
// tombstone (the entity has been removed).
func (l *Local) LiveEntity(path string) (EntityRecord, error) {
	row, err := orm.NewRepo[entityRow](l.db).
		Where("path = ?", path).
		OrderBy("generation DESC").
		First(context.Background())
	if liteorm.IsNotFound(err) {
		return EntityRecord{}, fmt.Errorf("%w: %s", ErrNoEntity, path)
	}
	if err != nil {
		return EntityRecord{}, fmt.Errorf("valiss: reading entity %q: %w", path, err)
	}
	if row.Tombstone {
		return EntityRecord{}, fmt.Errorf("%w: %s", ErrNoEntity, path)
	}
	return recordOf(row), nil
}

// EntityExists reports whether a live entity exists at path.
func (l *Local) EntityExists(path string) (bool, error) {
	_, err := l.LiveEntity(path)
	if errors.Is(err, ErrNoEntity) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListChildren returns the live entities of the given kind whose parent is
// parentPath, ordered by path.
func (l *Local) ListChildren(kind, parentPath string) ([]EntityRecord, error) {
	rows, err := orm.NewRepo[entityRow](l.db).
		Where("kind = ? AND parent = ?", kind, parentPath).
		OrderBy("path ASC, generation DESC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: listing %ss under %q: %w", kind, parentPath, err)
	}
	return liveByPath(rows), nil
}

// Subtree returns the live entities at path and everything beneath it, ordered
// by path. It is the basis of a removal's blast radius.
func (l *Local) Subtree(path string) ([]EntityRecord, error) {
	rows, err := orm.NewRepo[entityRow](l.db).
		Where("path = ? OR path LIKE ?", path, path+"/%").
		OrderBy("path ASC, generation DESC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: reading subtree %q: %w", path, err)
	}
	return liveByPath(rows), nil
}

// liveByPath reduces generation rows (pre-ordered path ASC, generation DESC) to
// the live entity per path: the first (highest-generation) row for each path,
// dropping tombstoned paths.
func liveByPath(rows []entityRow) []EntityRecord {
	var out []EntityRecord
	seen := make(map[string]bool)
	for _, row := range rows {
		if seen[row.Path] {
			continue
		}
		seen[row.Path] = true
		if row.Tombstone {
			continue
		}
		out = append(out, recordOf(row))
	}
	return out
}

// recordOf converts a persisted row to an EntityRecord.
func recordOf(row entityRow) EntityRecord {
	return EntityRecord{
		Kind:       row.Kind,
		Path:       row.Path,
		Parent:     row.Parent,
		Name:       row.Name,
		PublicKey:  row.PublicKey,
		Seed:       row.Seed,
		Generation: row.Generation,
		Epoch:      row.Epoch,
		Token:      row.Token,
		CreatedAt:  row.CreatedAt,
		Tombstone:  row.Tombstone,
	}
}

// nonZeroTime defaults a zero timestamp to now (UTC).
func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
