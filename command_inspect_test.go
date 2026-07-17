package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nkeys"
	"valiss.dev/valiss"
)

// mintOperatorToken mints a self-signed operator token for tests and returns
// the token and the operator public key.
func mintOperatorToken(t *testing.T, opts ...valiss.IssueOption) (string, string) {
	t.Helper()
	kp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatalf("CreateOperator: %v", err)
	}
	pub, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	tok, err := valiss.IssueOperator(kp, opts...)
	if err != nil {
		t.Fatalf("IssueOperator: %v", err)
	}
	return tok, pub
}

func TestInspectTokenDecodesClaims(t *testing.T) {
	tok, pub := mintOperatorToken(t,
		valiss.WithName("acme"),
		valiss.WithEpoch(3),
		valiss.WithTTL(time.Hour),
	)

	view, err := inspectToken(tok)
	if err != nil {
		t.Fatalf("inspectToken: %v", err)
	}
	if view.Type != "operator" {
		t.Errorf("Type = %q, want operator", view.Type)
	}
	if view.Name != "acme" {
		t.Errorf("Name = %q, want acme", view.Name)
	}
	if view.Subject != pub || view.Issuer != pub {
		t.Errorf("Issuer/Subject = %q/%q, want self-signed %q", view.Issuer, view.Subject, pub)
	}
	if view.Epoch != 3 {
		t.Errorf("Epoch = %d, want 3", view.Epoch)
	}
	if view.JTI == "" {
		t.Error("JTI is empty")
	}
	if view.Expires == "" {
		t.Error("Expires is empty for a TTL token")
	}
	if view.Signature != "valid" {
		t.Errorf("Signature = %q, want valid", view.Signature)
	}
	if view.Header.Ver != 1 || view.Header.Alg != "ed25519-nkey" {
		t.Errorf("Header = %+v, want ver 1 / ed25519-nkey", view.Header)
	}
}

func TestInspectTokenTamperedSignature(t *testing.T) {
	tok, _ := mintOperatorToken(t, valiss.WithName("acme"))
	parts := strings.Split(tok, ".")
	// Flip the last character of the signature segment to break verification
	// while keeping the token structurally well-formed.
	sig := []byte(parts[2])
	if sig[len(sig)-1] == 'A' {
		sig[len(sig)-1] = 'B'
	} else {
		sig[len(sig)-1] = 'A'
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)

	view, err := inspectToken(tampered)
	if err != nil {
		t.Fatalf("inspectToken on tampered token: %v", err)
	}
	if view.Signature != "invalid" {
		t.Errorf("Signature = %q, want invalid", view.Signature)
	}
}

func TestInspectTokenMalformed(t *testing.T) {
	for _, tok := range []string{"", "not-a-token", "a.b", "a.b.c.d"} {
		if _, err := inspectToken(tok); err == nil {
			t.Errorf("inspectToken(%q) = nil error, want malformed error", tok)
		}
	}
}

func TestInspectTextOutput(t *testing.T) {
	tok, _ := mintOperatorToken(t, valiss.WithName("acme"), valiss.WithEpoch(2))
	view, err := inspectToken(tok)
	if err != nil {
		t.Fatalf("inspectToken: %v", err)
	}
	var buf bytes.Buffer
	if err := view.writeText(&buf); err != nil {
		t.Fatalf("writeText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"type:", "operator", "name:", "acme", "epoch:", "signature:", "valid"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
}
