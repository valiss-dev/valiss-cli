package store

import (
	"errors"
	"testing"
	"time"
)

func mustInit(t *testing.T, dir, op string, pass []byte, cfg Config) *Local {
	t.Helper()
	l, err := Init(dir, op, pass, cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return l
}

func TestInitAndReopen(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("correct horse battery staple")
	cfg := Config{AuditRetention: 2160 * time.Hour}

	l := mustInit(t, dir, "acme", pass, cfg)
	info, err := l.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Operator != "acme" {
		t.Errorf("Operator = %q, want acme", info.Operator)
	}
	if info.SpecVersion != SpecVersion || info.WireVersion != WireVersion {
		t.Errorf("versions = %d/%d, want %d/%d", info.SpecVersion, info.WireVersion, SpecVersion, WireVersion)
	}
	if info.AuditRetention != cfg.AuditRetention {
		t.Errorf("AuditRetention = %v, want %v", info.AuditRetention, cfg.AuditRetention)
	}
	// The store.init audit entry is present.
	if info.AuditLines < 1 {
		t.Errorf("AuditLines = %d, want >= 1 (the store.init entry)", info.AuditLines)
	}
	if info.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", info.SizeBytes)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen with the same passphrase.
	l2, err := Open(dir, "acme", pass)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l2.Close()
	entries, err := l2.Audit("")
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(entries) == 0 || entries[0].Op != AuditStoreInit {
		t.Fatalf("Audit newest = %+v, want a %s entry", entries, AuditStoreInit)
	}
}

func TestInitExists(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("pw")
	l := mustInit(t, dir, "acme", pass, Config{})
	l.Close()
	if _, err := Init(dir, "acme", pass, Config{}); !errors.Is(err, ErrExists) {
		t.Errorf("Init on existing store = %v, want ErrExists", err)
	}
}

func TestOpenNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, "ghost", []byte("pw")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Open missing store = %v, want ErrNotFound", err)
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("right passphrase"), Config{})
	l.Close()
	if _, err := Open(dir, "acme", []byte("wrong passphrase")); err == nil {
		t.Error("Open with wrong passphrase succeeded; want failure")
	}
}

func TestSetConfig(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{AuditRetention: 2160 * time.Hour})
	defer l.Close()

	if err := l.SetConfig("audit-retention", "720h"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	cfg, err := l.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if cfg.AuditRetention != 720*time.Hour {
		t.Errorf("AuditRetention = %v, want 720h", cfg.AuditRetention)
	}
	// Unknown key and bad value are rejected.
	if err := l.SetConfig("bogus", "x"); err == nil {
		t.Error("SetConfig(bogus) = nil, want error")
	}
	if err := l.SetConfig("audit-retention", "not-a-duration"); err == nil {
		t.Error("SetConfig(bad duration) = nil, want error")
	}
}

func TestAuditSubtreeScoping(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{})
	defer l.Close()

	now := time.Now().UTC()
	for _, e := range []AuditEntry{
		{At: now, Op: AuditEntityAdd, Path: "acme"},
		{At: now, Op: AuditEntityAdd, Path: "acme/team"},
		{At: now, Op: AuditEntityAdd, Path: "acme/team/bob"},
		{At: now, Op: AuditEntityAdd, Path: "acme/other"},
	} {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// The account subtree covers the account and its users, not siblings.
	got, err := l.Audit("acme/team")
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	var paths []string
	for _, e := range got {
		paths = append(paths, e.Path)
	}
	if len(got) != 2 {
		t.Fatalf("Audit(acme/team) returned %d entries (%v), want 2", len(got), paths)
	}
	for _, e := range got {
		if e.Path != "acme/team" && e.Path != "acme/team/bob" {
			t.Errorf("unexpected path %q in acme/team subtree", e.Path)
		}
	}
}

func TestAuditRetentionSweep(t *testing.T) {
	dir := t.TempDir()
	l := mustInit(t, dir, "acme", []byte("pw"), Config{AuditRetention: time.Hour})
	// An old entry, well outside the one-hour window.
	if err := l.Append(AuditEntry{At: time.Now().UTC().Add(-48 * time.Hour), Op: AuditEntityAdd, Path: "acme"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	l.Close()

	// Reopen triggers the lazy sweep; the old entry is gone, recent ones stay.
	l2, err := Open(dir, "acme", []byte("pw"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l2.Close()
	entries, err := l2.Audit("")
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	for _, e := range entries {
		if e.At.Before(time.Now().UTC().Add(-time.Hour - time.Minute)) {
			t.Errorf("stale entry survived the sweep: %+v", e)
		}
	}
}
