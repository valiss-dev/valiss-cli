package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nkeys"

	"valiss.dev/cli/valiss/internal/store"
	"valiss.dev/valiss"
)

// The chain-orchestration layer sits between the command tree and the store:
// it generates nkeys, mints tokens with the valiss library, and persists the
// result through the store's data methods. Keeping it here keeps the store
// package free of the crypto stack.
//
// Generation/epoch reconciliation (a judgement call ADR 0021 leaves implicit).
// ADR 0021 gives every entity a "generation" lifecycle counter; the valiss
// wire carries a trust-domain "epoch." We map them at the operator: an
// operator's generation and epoch advance together (generation N is epoch N,
// starting at 1), and account and user tokens are stamped with the operator's
// current epoch while carrying their own independent generation counters. The
// per-entity generation floors ADR 0022 will later reflect on the wire are not
// stamped yet (that is valiss-go 0.14 / a future wire step).

// entitySummary is the display view of an entity for show and list output.
type entitySummary struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	PublicKey  string `json:"public_key"`
	Generation uint64 `json:"generation"`
	Epoch      uint64 `json:"epoch"`
	JTI        string `json:"jti,omitempty"`
	Created    string `json:"created,omitempty"`
}

// summarize builds a display summary from a persisted entity, decoding the
// entity's token for its jti.
func summarize(rec store.EntityRecord) entitySummary {
	s := entitySummary{
		Kind:       rec.Kind,
		Path:       rec.Path,
		Name:       rec.Name,
		PublicKey:  rec.PublicKey,
		Generation: rec.Generation,
		Epoch:      rec.Epoch,
	}
	if !rec.CreatedAt.IsZero() {
		s.Created = rec.CreatedAt.UTC().Format(time.RFC3339)
	}
	if rec.Token != "" {
		if claims, err := valiss.Decode(rec.Token); err == nil {
			s.JTI = claims.ID
		}
	}
	return s
}

// addOperator creates the operator identity in a store: a fresh operator nkey,
// a self-signed operator token at generation/epoch 1, persisted as the first
// entity generation. It fails if the store already holds a live operator.
func addOperator(st *store.Local, name string) (store.EntityRecord, error) {
	if exists, err := st.EntityExists(name); err != nil {
		return store.EntityRecord{}, err
	} else if exists {
		return store.EntityRecord{}, fmt.Errorf("valiss: operator %q already exists", name)
	}

	kp, err := nkeys.CreateOperator()
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: generating operator key: %w", err)
	}
	pub, seed, err := keyMaterial(kp)
	if err != nil {
		return store.EntityRecord{}, err
	}
	const gen, epoch = 1, 1
	token, err := valiss.IssueOperator(kp, valiss.WithName(name), valiss.WithEpoch(epoch))
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: minting operator token: %w", err)
	}
	rec := store.EntityRecord{
		Kind:       store.KindOperator,
		Path:       name,
		Parent:     "",
		Name:       name,
		PublicKey:  pub,
		Seed:       seed,
		Generation: gen,
		Epoch:      epoch,
		Token:      token,
		CreatedAt:  time.Now().UTC(),
	}
	if err := st.PutEntity(rec); err != nil {
		return store.EntityRecord{}, err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditEntityAdd, Path: name,
		Detail: fmt.Sprintf("operator %s gen=%d epoch=%d", pub, gen, epoch)}); err != nil {
		return store.EntityRecord{}, err
	}
	return rec, nil
}

// addAccount creates an account identity under an operator: a fresh account
// key and an operator-signed account token at the operator's current epoch,
// carrying its own generation 1. The account token is not deposited in the
// allowlist here; depositing an account's jti is a deliberate act through the
// allowlist verb (this keeps account add fail-closed and parallel to operator
// add). It fails if the operator is absent or the account already exists.
func addAccount(st *store.Local, opPath, acctName string) (store.EntityRecord, error) {
	op, err := st.LiveEntity(opPath)
	if errors.Is(err, store.ErrNoEntity) {
		return store.EntityRecord{}, errNoOperator
	} else if err != nil {
		return store.EntityRecord{}, err
	}
	path := opPath + "/" + acctName
	if exists, err := st.EntityExists(path); err != nil {
		return store.EntityRecord{}, err
	} else if exists {
		return store.EntityRecord{}, fmt.Errorf("valiss: account %q already exists", path)
	}

	opKP, err := nkeys.FromSeed(op.Seed)
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: loading operator key: %w", err)
	}
	kp, err := nkeys.CreateAccount()
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: generating account key: %w", err)
	}
	pub, seed, err := keyMaterial(kp)
	if err != nil {
		return store.EntityRecord{}, err
	}
	token, err := valiss.IssueAccount(opKP, pub, valiss.WithName(acctName), valiss.WithEpoch(op.Epoch))
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: minting account token: %w", err)
	}
	rec := store.EntityRecord{
		Kind:       store.KindAccount,
		Path:       path,
		Parent:     opPath,
		Name:       acctName,
		PublicKey:  pub,
		Seed:       seed,
		Generation: 1,
		Epoch:      op.Epoch,
		Token:      token,
		CreatedAt:  time.Now().UTC(),
	}
	if err := st.PutEntity(rec); err != nil {
		return store.EntityRecord{}, err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditEntityAdd, Path: path,
		Detail: fmt.Sprintf("account %s gen=1 epoch=%d", pub, op.Epoch)}); err != nil {
		return store.EntityRecord{}, err
	}
	return rec, nil
}

// rotateOperator performs an epoch rotation: it keeps the operator key (the
// pinned trust anchor) and advances the epoch/generation, re-minting the
// self-signed operator token at the new epoch. Verifiers that adopt the new
// operator token stop accepting account and user tokens at the old epoch, so
// the whole domain rotates once the new operator token is distributed.
//
// Judgement call (ADR 0021 rotate is ambiguous). ADR 0021 phrases rotate as
// retiring a "signing key," which could mean replacing the operator key. We
// read it as epoch rotation for two reasons: it is the only rotation verb, so
// it must cover the documented rotation ceremony, which is epoch-based; and
// the keyring grace-period model is "one operator key at several epochs,"
// which is same-key epoch rotation. Replacing the operator key (the
// seed-compromise remedy that re-pins the anchor everywhere) is a distinct,
// heavier operation not modeled by this verb.
func rotateOperator(st *store.Local) (store.EntityRecord, error) {
	cur, err := st.LiveEntity(operatorPathOf(st))
	if err != nil {
		return store.EntityRecord{}, err
	}
	kp, err := nkeys.FromSeed(cur.Seed)
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: loading operator key: %w", err)
	}
	next := cur
	next.Generation = cur.Generation + 1
	next.Epoch = cur.Epoch + 1
	next.CreatedAt = time.Now().UTC()
	token, err := valiss.IssueOperator(kp, valiss.WithName(cur.Name), valiss.WithEpoch(next.Epoch))
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: re-minting operator token: %w", err)
	}
	next.Token = token
	if err := st.PutEntity(next); err != nil {
		return store.EntityRecord{}, err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditOperatorRotate, Path: cur.Path,
		Detail: fmt.Sprintf("epoch %d -> %d", cur.Epoch, next.Epoch)}); err != nil {
		return store.EntityRecord{}, err
	}
	return next, nil
}

// removeEntity tombstones an entity and its subtree and revokes the subtree's
// live tokens, returning the blast radius: the entities that fall and the
// number of live tokens revoked. The caller confirms before calling.
func removeEntity(st *store.Local, path string) (fallen []store.EntityRecord, revoked int, err error) {
	fallen, err = st.Subtree(path)
	if err != nil {
		return nil, 0, err
	}
	if len(fallen) == 0 {
		return nil, 0, fmt.Errorf("%w: %s", store.ErrNoEntity, path)
	}
	revoked, err = st.RevokeJTIsUnder(path, time.Now().UTC())
	if err != nil {
		return nil, 0, err
	}
	if _, err = st.TombstoneSubtree(path); err != nil {
		return nil, 0, err
	}
	if err = st.Append(store.AuditEntry{Op: store.AuditEntityRemove, Path: path,
		Detail: fmt.Sprintf("removed %d entities, revoked %d tokens", len(fallen), revoked)}); err != nil {
		return nil, 0, err
	}
	return fallen, revoked, nil
}

// blastRadius returns the entities that would fall and the count of live tokens
// that would be revoked if path were removed, without changing anything.
func blastRadius(st *store.Local, path string) (fallen []store.EntityRecord, tokens int, err error) {
	fallen, err = st.Subtree(path)
	if err != nil {
		return nil, 0, err
	}
	jtis, err := st.LiveJTIsUnder(path)
	if err != nil {
		return nil, 0, err
	}
	return fallen, len(jtis), nil
}

// keyMaterial extracts the public key and seed from a key pair.
func keyMaterial(kp nkeys.KeyPair) (pub string, seed []byte, err error) {
	if pub, err = kp.PublicKey(); err != nil {
		return "", nil, fmt.Errorf("valiss: reading public key: %w", err)
	}
	if seed, err = kp.Seed(); err != nil {
		return "", nil, fmt.Errorf("valiss: reading seed: %w", err)
	}
	return pub, seed, nil
}

// operatorPathOf returns the operator path a store is keyed by. A store holds
// exactly one operator, so this is the store's operator name.
func operatorPathOf(st *store.Local) string { return st.Operator() }

// operatorOf returns the operator (first) segment of an entity path.
func operatorOf(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

var errNoOperator = errors.New("valiss: store has no operator; run 'valiss operator add <operator>' first")
