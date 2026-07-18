package store

import (
	"context"
	"fmt"
	"time"

	"liteorm.org"
	"liteorm.org/orm"
)

// AllowlistEntry is one jti in the local allowlist, with the time it was
// deposited. The allowlist is the fail-closed revocation surface: a server
// accepts only tokens whose jti sits here, and export renders the set in the
// newline-delimited form valiss-go's LoadAllowlistFile consumes (ADR 0021).
type AllowlistEntry struct {
	JTI     string
	AddedAt time.Time
}

// AddAllowlist deposits a jti in the allowlist. It reports whether the jti was
// newly added; a jti already present is left untouched and reports false, so
// re-adding is idempotent.
func (l *Local) AddAllowlist(jti string, at time.Time) (bool, error) {
	present, err := l.AllowlistContains(jti)
	if err != nil {
		return false, err
	}
	if present {
		return false, nil
	}
	row := allowlistRow{JTI: jti, AddedAt: nonZeroTime(at)}
	if err := orm.NewRepo[allowlistRow](l.db).Create(context.Background(), &row); err != nil {
		return false, fmt.Errorf("valiss: adding %q to allowlist: %w", jti, err)
	}
	return true, nil
}

// RemoveAllowlist removes a jti from the allowlist. It reports whether a row
// was removed; a jti that was absent reports false.
func (l *Local) RemoveAllowlist(jti string) (bool, error) {
	present, err := l.AllowlistContains(jti)
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

// AllowlistContains reports whether a jti is in the allowlist.
func (l *Local) AllowlistContains(jti string) (bool, error) {
	_, err := orm.NewRepo[allowlistRow](l.db).
		Where("jti = ?", jti).
		First(context.Background())
	if liteorm.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("valiss: checking allowlist for %q: %w", jti, err)
	}
	return true, nil
}

// ListAllowlist returns the allowlist entries, oldest deposit first.
func (l *Local) ListAllowlist() ([]AllowlistEntry, error) {
	rows, err := orm.NewRepo[allowlistRow](l.db).
		OrderBy("added_at ASC, jti ASC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: listing allowlist: %w", err)
	}
	out := make([]AllowlistEntry, len(rows))
	for i, r := range rows {
		out[i] = AllowlistEntry{JTI: r.JTI, AddedAt: r.AddedAt}
	}
	return out, nil
}
