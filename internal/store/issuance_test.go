package store

import (
	"errors"
	"testing"
	"time"
)

func TestTokenRecords(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{})
	defer l.Close()

	base := time.Now().UTC().Truncate(time.Second)
	recs := []TokenRecord{
		{JTI: "jti-a", Subject: "acme/team/alice", Level: KindUser, Token: "tok-a", MintedAt: base.Add(-2 * time.Hour)},
		{JTI: "jti-b", Subject: "acme/team/bob", Level: KindUser, Token: "tok-b", MintedAt: base.Add(-1 * time.Hour),
			TemplateName: "web", TemplateGen: 2, TemplateHash: "deadbeef", ExpiresAt: base.Add(time.Hour)},
		{JTI: "jti-c", Subject: "acme/ops/carol", Level: KindUser, Token: "tok-c", MintedAt: base},
	}
	for _, r := range recs {
		if err := l.PutToken(r); err != nil {
			t.Fatalf("PutToken %s: %v", r.JTI, err)
		}
	}

	// Token by jti, and the not-found error.
	got, err := l.Token("jti-b")
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.TemplateName != "web" || got.TemplateGen != 2 || got.TemplateHash != "deadbeef" {
		t.Errorf("template stamp = %q/%d/%q, want web/2/deadbeef", got.TemplateName, got.TemplateGen, got.TemplateHash)
	}
	if !got.ExpiresAt.Equal(base.Add(time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, base.Add(time.Hour))
	}
	if _, err := l.Token("nope"); !errors.Is(err, ErrNoToken) {
		t.Errorf("Token(nope) = %v, want ErrNoToken", err)
	}

	// ListTokens is subtree-scoped, newest mint first.
	under, err := l.ListTokens("acme/team")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(under) != 2 {
		t.Fatalf("ListTokens(acme/team) = %d records, want 2", len(under))
	}
	if under[0].JTI != "jti-b" || under[1].JTI != "jti-a" {
		t.Errorf("ListTokens order = %s,%s, want jti-b,jti-a", under[0].JTI, under[1].JTI)
	}
	all, err := l.ListTokens("acme")
	if err != nil {
		t.Fatalf("ListTokens(acme): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListTokens(acme) = %d, want 3", len(all))
	}

	// RevokeToken stamps the revocation in place.
	if err := l.RevokeToken("jti-a", base); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	revoked, err := l.Token("jti-a")
	if err != nil {
		t.Fatalf("Token after revoke: %v", err)
	}
	if !revoked.Revoked || revoked.RevokedAt.IsZero() {
		t.Errorf("jti-a revoked = %v at %v, want revoked with a timestamp", revoked.Revoked, revoked.RevokedAt)
	}
}

// TestPutTokenIdempotent asserts a jti maps to exactly one record: re-putting
// identical content is a no-op, while a jti reused for a different token blob
// is rejected rather than duplicated (the unique constraint's invariant).
func TestPutTokenIdempotent(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{})
	defer l.Close()

	rec := TokenRecord{JTI: "jti-x", Subject: "acme/team", Level: KindAccount, Token: "tok-x", MintedAt: time.Now().UTC()}
	if err := l.PutToken(rec); err != nil {
		t.Fatalf("PutToken: %v", err)
	}
	// A second identical put is a no-op, not a duplicate row or an error.
	if err := l.PutToken(rec); err != nil {
		t.Fatalf("PutToken idempotent: %v", err)
	}
	all, err := l.ListTokens("acme")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ListTokens = %d rows, want 1 (identical re-mint must not duplicate)", len(all))
	}
	// The same jti with a different token blob is a collision, rejected.
	if err := l.PutToken(TokenRecord{JTI: "jti-x", Subject: "acme/team", Level: KindAccount, Token: "different", MintedAt: time.Now().UTC()}); err == nil {
		t.Error("PutToken with a colliding jti and different content succeeded; want rejection")
	}
}

func TestAllowlistOps(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{})
	defer l.Close()

	// Add is idempotent: first add reports true, a repeat reports false.
	if added, err := l.AddAllowlist("jti-1", time.Now().UTC()); err != nil || !added {
		t.Fatalf("AddAllowlist(jti-1) = %v,%v, want true,nil", added, err)
	}
	if added, err := l.AddAllowlist("jti-1", time.Now().UTC()); err != nil || added {
		t.Fatalf("AddAllowlist(jti-1) repeat = %v,%v, want false,nil", added, err)
	}
	if _, err := l.AddAllowlist("jti-2", time.Now().UTC()); err != nil {
		t.Fatalf("AddAllowlist(jti-2): %v", err)
	}

	if present, err := l.AllowlistContains("jti-1"); err != nil || !present {
		t.Errorf("AllowlistContains(jti-1) = %v,%v, want true", present, err)
	}
	if present, err := l.AllowlistContains("absent"); err != nil || present {
		t.Errorf("AllowlistContains(absent) = %v,%v, want false", present, err)
	}

	entries, err := l.ListAllowlist()
	if err != nil {
		t.Fatalf("ListAllowlist: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ListAllowlist = %d, want 2", len(entries))
	}

	// Remove reports whether a row fell.
	if removed, err := l.RemoveAllowlist("jti-1"); err != nil || !removed {
		t.Errorf("RemoveAllowlist(jti-1) = %v,%v, want true", removed, err)
	}
	if removed, err := l.RemoveAllowlist("jti-1"); err != nil || removed {
		t.Errorf("RemoveAllowlist(jti-1) repeat = %v,%v, want false", removed, err)
	}
}
