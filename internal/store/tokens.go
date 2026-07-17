package store

import (
	"context"
	"fmt"
	"time"

	"liteorm.org/orm"
)

// LiveJTIsUnder returns the jtis of live (not-revoked) tokens whose subject is
// at or under path. It is the token half of a removal's blast radius.
//
// "Live" here means not revoked; expiry is not yet factored in (the token verb
// family, which records expiry meaningfully, refines this). Until token mint
// lands the tokens table is empty and this is always nil.
func (l *Local) LiveJTIsUnder(path string) ([]string, error) {
	rows, err := orm.NewRepo[tokenRow](l.db).
		Where("(subject = ? OR subject LIKE ?) AND revoked = 0", path, path+"/%").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: reading live tokens under %q: %w", path, err)
	}
	jtis := make([]string, len(rows))
	for i, r := range rows {
		jtis[i] = r.JTI
	}
	return jtis, nil
}

// RevokeJTIsUnder marks the live tokens under path revoked and removes their
// jtis from the allowlist (revocation is a jti leaving the allowlist). It
// returns the number of tokens revoked.
func (l *Local) RevokeJTIsUnder(path string, at time.Time) (int, error) {
	jtis, err := l.LiveJTIsUnder(path)
	if err != nil {
		return 0, err
	}
	ctx := context.Background()
	if _, err := l.db.ExecContext(ctx,
		"UPDATE tokens SET revoked = 1, revoked_at = ? WHERE (subject = ? OR subject LIKE ?) AND revoked = 0",
		nonZeroTime(at), path, path+"/%"); err != nil {
		return 0, fmt.Errorf("valiss: revoking tokens under %q: %w", path, err)
	}
	for _, jti := range jtis {
		if _, err := l.db.ExecContext(ctx, "DELETE FROM allowlist WHERE jti = ?", jti); err != nil {
			return 0, fmt.Errorf("valiss: removing %q from allowlist: %w", jti, err)
		}
	}
	return len(jtis), nil
}

// TombstoneSubtree appends a tombstone generation for every live entity at or
// under path, bumping each one's generation. After it returns, LiveEntity for
// each of those paths reports ErrNoEntity. The caller is responsible for
// revoking the subtree's live tokens (RevokeJTIsUnder) and for the audit
// journal.
func (l *Local) TombstoneSubtree(path string) ([]EntityRecord, error) {
	live, err := l.Subtree(path)
	if err != nil {
		return nil, err
	}
	for _, e := range live {
		e.Generation++
		e.Tombstone = true
		e.CreatedAt = time.Now().UTC()
		if err := l.PutEntity(e); err != nil {
			return nil, err
		}
	}
	return live, nil
}
