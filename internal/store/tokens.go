package store

import (
	"context"
	"fmt"
	"time"

	"liteorm.org"
	"liteorm.org/orm"
)

// TokenRecord is one issuance: a minted token identified by its jti, the
// entity it was minted for, the template stamp a mint used (name, generation,
// content hash), and the lifecycle timestamps. Tokens are revoked, never
// removed, so a revoked token keeps its record with the revocation stamped
// (ADR 0021).
type TokenRecord struct {
	JTI          string
	Subject      string
	Level        string
	Token        string
	TemplateName string
	TemplateGen  uint64
	TemplateHash string
	MintedAt     time.Time
	ExpiresAt    time.Time
	Revoked      bool
	RevokedAt    time.Time
}

// PutToken records an issuance. A token row is written once at mint; a later
// revocation updates the row in place (RevokeToken) rather than appending, so
// a jti maps to exactly one record.
func (l *Local) PutToken(r TokenRecord) error {
	row := tokenRow{
		JTI:          r.JTI,
		Subject:      r.Subject,
		Level:        r.Level,
		Token:        r.Token,
		TemplateName: r.TemplateName,
		TemplateGen:  r.TemplateGen,
		TemplateHash: r.TemplateHash,
		MintedAt:     nonZeroTime(r.MintedAt),
		ExpiresAt:    r.ExpiresAt,
		Revoked:      r.Revoked,
		RevokedAt:    r.RevokedAt,
	}
	if err := orm.NewRepo[tokenRow](l.db).Create(context.Background(), &row); err != nil {
		return fmt.Errorf("valiss: writing token %q: %w", r.JTI, err)
	}
	return nil
}

// Token returns the issuance record for a jti, or ErrNoToken.
func (l *Local) Token(jti string) (TokenRecord, error) {
	row, err := orm.NewRepo[tokenRow](l.db).
		Where("jti = ?", jti).
		First(context.Background())
	if liteorm.IsNotFound(err) {
		return TokenRecord{}, fmt.Errorf("%w: %s", ErrNoToken, jti)
	}
	if err != nil {
		return TokenRecord{}, fmt.Errorf("valiss: reading token %q: %w", jti, err)
	}
	return tokenRecordOf(row), nil
}

// ListTokens returns the issuance records whose subject is at or under
// pathPrefix, newest mint first. It is the token verb family's read path,
// scoped to the addressed entity's subtree.
func (l *Local) ListTokens(pathPrefix string) ([]TokenRecord, error) {
	rows, err := orm.NewRepo[tokenRow](l.db).
		Where("subject = ? OR subject LIKE ?", pathPrefix, pathPrefix+"/%").
		OrderBy("minted_at DESC, jti ASC").
		Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("valiss: listing tokens under %q: %w", pathPrefix, err)
	}
	out := make([]TokenRecord, len(rows))
	for i, r := range rows {
		out[i] = tokenRecordOf(r)
	}
	return out, nil
}

// RevokeToken marks a single token revoked. It does not touch the allowlist;
// the caller removes the jti, since revocation is a jti leaving the allowlist
// (ADR 0021).
func (l *Local) RevokeToken(jti string, at time.Time) error {
	if _, err := l.db.ExecContext(context.Background(),
		"UPDATE tokens SET revoked = 1, revoked_at = ? WHERE jti = ?", nonZeroTime(at), jti); err != nil {
		return fmt.Errorf("valiss: revoking token %q: %w", jti, err)
	}
	return nil
}

// tokenRecordOf converts a persisted row to a TokenRecord.
func tokenRecordOf(row tokenRow) TokenRecord {
	return TokenRecord{
		JTI:          row.JTI,
		Subject:      row.Subject,
		Level:        row.Level,
		Token:        row.Token,
		TemplateName: row.TemplateName,
		TemplateGen:  row.TemplateGen,
		TemplateHash: row.TemplateHash,
		MintedAt:     row.MintedAt,
		ExpiresAt:    row.ExpiresAt,
		Revoked:      row.Revoked,
		RevokedAt:    row.RevokedAt,
	}
}

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
