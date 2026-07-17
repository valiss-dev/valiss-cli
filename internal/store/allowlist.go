package store

import (
	"context"
	"fmt"
	"time"

	"liteorm.org/orm"
)

// AllowlistRecord is one deposited jti and when it was added.
type AllowlistRecord struct {
	JTI     string
	AddedAt time.Time
}

// ListAllowlist returns the deposited jtis, ordered by jti so the export is
// stable across runs.
func (l *Local) ListAllowlist() ([]AllowlistRecord, error) {
	rows, err := orm.NewRepo[allowlistRow](l.db).OrderBy("jti ASC").Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: listing allowlist: %w", err)
	}
	out := make([]AllowlistRecord, len(rows))
	for i, r := range rows {
		out[i] = AllowlistRecord{JTI: r.JTI, AddedAt: r.AddedAt}
	}
	return out, nil
}

// HasAllowlist reports whether a jti is deposited.
func (l *Local) HasAllowlist(jti string) (bool, error) {
	ok, err := orm.NewRepo[allowlistRow](l.db).Where("jti = ?", jti).Exists(context.Background())
	if err != nil {
		return false, fmt.Errorf("valiss: checking allowlist: %w", err)
	}
	return ok, nil
}

// RemoveAllowlist removes a jti from the allowlist, reporting whether a row was
// actually removed.
func (l *Local) RemoveAllowlist(jti string) (bool, error) {
	present, err := l.HasAllowlist(jti)
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	if _, err := l.db.ExecContext(context.Background(), "DELETE FROM allowlist WHERE jti = ?", jti); err != nil {
		return false, fmt.Errorf("valiss: removing %q from allowlist: %w", jti, err)
	}
	return true, nil
}
