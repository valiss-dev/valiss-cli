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
// carrying its own generation 1. The account token is recorded as an issuance
// (its jti is the allowlist key valiss-go verifies against, per
// docs/concepts/allowlist.md), so the token verbs can address and revoke it.
// Depositing the jti in the allowlist is the command layer's job (default on,
// --no-allowlist to opt out). It fails if the operator is absent or the account
// already exists.
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
	if err := recordAccountIssuance(st, rec); err != nil {
		return store.EntityRecord{}, err
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditEntityAdd, Path: path,
		Detail: fmt.Sprintf("account %s gen=1 epoch=%d", pub, op.Epoch)}); err != nil {
		return store.EntityRecord{}, err
	}
	return rec, nil
}

// recordAccountIssuance stores an account entity's token as an issuance record,
// keyed by its jti. The account jti is what a server's allowlist consults
// (verifier.go checks account.ID), so recording it as an issuance is what lets
// the token verbs address, list, and revoke the account credential.
func recordAccountIssuance(st *store.Local, acct store.EntityRecord) error {
	claims, err := valiss.Decode(acct.Token)
	if err != nil {
		return fmt.Errorf("valiss: decoding account token: %w", err)
	}
	return st.PutToken(store.TokenRecord{
		JTI:       claims.ID,
		Subject:   acct.Path,
		Level:     store.KindAccount,
		Token:     acct.Token,
		MintedAt:  claims.IssuedAt,
		ExpiresAt: claims.ExpiresAt,
	})
}

// addUser creates a user identity under an account: a fresh user key and an
// account-signed user token, carrying its own generation 1. The token is
// stamped with the operator's current epoch, not the account row's epoch, so a
// freshly added user is at the domain's current epoch. (Re-minting accounts
// and users after an operator rotation — the rotation ceremony — is not yet
// automated; a stale account token at an older epoch is a ceremony concern.)
func addUser(st *store.Local, acctPath, userName string) (store.EntityRecord, error) {
	acct, err := st.LiveEntity(acctPath)
	if errors.Is(err, store.ErrNoEntity) {
		return store.EntityRecord{}, fmt.Errorf("valiss: account %q not found", acctPath)
	} else if err != nil {
		return store.EntityRecord{}, err
	}
	op, err := st.LiveEntity(operatorOf(acctPath))
	if err != nil {
		return store.EntityRecord{}, err
	}
	path := acctPath + "/" + userName
	if exists, err := st.EntityExists(path); err != nil {
		return store.EntityRecord{}, err
	} else if exists {
		return store.EntityRecord{}, fmt.Errorf("valiss: user %q already exists", path)
	}

	acctKP, err := nkeys.FromSeed(acct.Seed)
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: loading account key: %w", err)
	}
	kp, err := nkeys.CreateUser()
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: generating user key: %w", err)
	}
	pub, seed, err := keyMaterial(kp)
	if err != nil {
		return store.EntityRecord{}, err
	}
	token, err := valiss.IssueUser(acctKP, pub, valiss.WithName(userName), valiss.WithEpoch(op.Epoch))
	if err != nil {
		return store.EntityRecord{}, fmt.Errorf("valiss: minting user token: %w", err)
	}
	rec := store.EntityRecord{
		Kind:       store.KindUser,
		Path:       path,
		Parent:     acctPath,
		Name:       userName,
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
		Detail: fmt.Sprintf("user %s gen=1 epoch=%d", pub, op.Epoch)}); err != nil {
		return store.EntityRecord{}, err
	}
	return rec, nil
}

// mintParams carries the resolved token mint inputs from the command's flags.
// The grant slices are the raw flag values; mintToken parses them into
// extension claims and unions them with a stamped template's grants.
type mintParams struct {
	template    string
	http        []string
	grpc        []string
	ext         []string
	ttl         time.Duration
	ttlSet      bool
	bearer      bool
	noExtension bool
}

// mintToken mints a fresh account-signed user token for the addressed user and
// records the issuance (ADR 0021). It resolves an optional template as a
// mint-time stamp (its name, generation, and content hash are recorded so the
// audit reads correctly after the template evolves), unions the template's
// grants with the explicit grant flags, reconciles TTL and the bearer flag,
// mints with the valiss library, and persists the issuance record. The jti is
// registered in the allowlist by the caller (unless opted out).
func mintToken(st *store.Local, path string, p mintParams) (store.TokenRecord, error) {
	user, err := st.LiveEntity(path)
	if errors.Is(err, store.ErrNoEntity) {
		return store.TokenRecord{}, fmt.Errorf("valiss: user %q not found", path)
	} else if err != nil {
		return store.TokenRecord{}, err
	}
	acct, err := st.LiveEntity(parentOf(path))
	if errors.Is(err, store.ErrNoEntity) {
		return store.TokenRecord{}, fmt.Errorf("valiss: account %q not found", parentOf(path))
	} else if err != nil {
		return store.TokenRecord{}, err
	}
	op, err := st.LiveEntity(operatorOf(path))
	if err != nil {
		return store.TokenRecord{}, err
	}

	grants := newGrantBuilder()
	var (
		tmplName string
		tmplGen  uint64
		tmplHash string
		bearer   = p.bearer
		ttl      = p.ttl
		ttlSet   = p.ttlSet
	)
	if p.template != "" {
		ref, err := parseBareTemplateRef(p.template)
		if err != nil {
			return store.TokenRecord{}, err
		}
		trec, err := resolveTemplate(st, ref)
		if err != nil {
			return store.TokenRecord{}, err
		}
		if trec.Retired {
			return store.TokenRecord{}, fmt.Errorf("valiss: template %q is retired and takes no new mints", ref.name)
		}
		if err := grants.addTemplateGrants(parseList(trec.HTTP), parseList(trec.GRPC), parseList(trec.Custom)); err != nil {
			return store.TokenRecord{}, err
		}
		if trec.Bearer {
			bearer = true
		}
		if !ttlSet && trec.TTLSeconds > 0 {
			ttl = time.Duration(trec.TTLSeconds) * time.Second
			ttlSet = true
		}
		tmplName, tmplGen, tmplHash = trec.Name, trec.Generation, trec.ContentHash
	}
	// Explicit grant flags union with the template's grants.
	for _, v := range p.http {
		if err := grants.addHTTPFlag(v); err != nil {
			return store.TokenRecord{}, err
		}
	}
	for _, v := range p.grpc {
		if err := grants.addGRPCFlag(v); err != nil {
			return store.TokenRecord{}, err
		}
	}
	for _, v := range p.ext {
		if err := grants.addExtFlag(v); err != nil {
			return store.TokenRecord{}, err
		}
	}

	opts := []valiss.IssueOption{valiss.WithName(user.Name), valiss.WithEpoch(op.Epoch)}
	if bearer {
		opts = append(opts, valiss.WithBearer())
	}
	if ttlSet && ttl > 0 {
		opts = append(opts, valiss.WithTTL(ttl))
	}
	opts = append(opts, grants.build()...)

	acctKP, err := nkeys.FromSeed(acct.Seed)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: loading account key: %w", err)
	}
	token, err := valiss.IssueUser(acctKP, user.PublicKey, opts...)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: minting user token: %w", err)
	}
	claims, err := valiss.Decode(token)
	if err != nil {
		return store.TokenRecord{}, fmt.Errorf("valiss: decoding minted token: %w", err)
	}
	rec := store.TokenRecord{
		JTI:          claims.ID,
		Subject:      path,
		Level:        store.KindUser,
		Token:        token,
		TemplateName: tmplName,
		TemplateGen:  tmplGen,
		TemplateHash: tmplHash,
		MintedAt:     claims.IssuedAt,
		ExpiresAt:    claims.ExpiresAt,
	}
	if err := st.PutToken(rec); err != nil {
		return store.TokenRecord{}, err
	}
	detail := fmt.Sprintf("user token jti=%s epoch=%d", rec.JTI, op.Epoch)
	if tmplName != "" {
		detail += fmt.Sprintf(" template=%s@%d", tmplName, tmplGen)
	}
	if err := st.Append(store.AuditEntry{Op: store.AuditTokenMint, Path: path, Detail: detail}); err != nil {
		return store.TokenRecord{}, err
	}
	return rec, nil
}

// rotationResult reports what an epoch rotation re-issued: the new operator
// record and how many accounts and users were re-minted at the new epoch.
type rotationResult struct {
	operator store.EntityRecord
	accounts int
	users    int
}

// rotateOperator performs a full epoch-rotation ceremony: it keeps the operator
// key (the pinned trust anchor), advances the epoch/generation, re-mints the
// self-signed operator token at the new epoch, and then re-issues every live
// account and user token beneath it at the new epoch. This is what makes the
// rotated domain usable: under WithOperatorToken, VerifyRequest requires the
// account and user epochs to echo the operator epoch (verifier.go), so a rotate
// that bumped only the operator token would leave the whole domain
// unverifiable. Re-issuing the descendants is the ceremony
// docs/concepts/rotation.md describes.
//
// Because an account token's jti is a content hash over its epoch, a re-issued
// account has a new jti. The ceremony swaps the allowlist entry (old jti out,
// new jti in) for every account that was allowlisted, so the exported allowlist
// keeps admitting the same accounts after rotation, and marks the superseded
// account issuance records revoked.
//
// Judgement call (ADR 0021 rotate is ambiguous). ADR 0021 phrases rotate as
// retiring a "signing key," which could mean replacing the operator key. We
// read it as epoch rotation for two reasons: it is the only rotation verb, so
// it must cover the documented rotation ceremony, which is epoch-based; and
// the keyring grace-period model is "one operator key at several epochs,"
// which is same-key epoch rotation. Replacing the operator key (the
// seed-compromise remedy that re-pins the anchor everywhere) is a distinct,
// heavier operation not modeled by this verb.
func rotateOperator(st *store.Local) (rotationResult, error) {
	cur, err := st.LiveEntity(operatorPathOf(st))
	if err != nil {
		return rotationResult{}, err
	}
	opKP, err := nkeys.FromSeed(cur.Seed)
	if err != nil {
		return rotationResult{}, fmt.Errorf("valiss: loading operator key: %w", err)
	}
	next := cur
	next.Generation = cur.Generation + 1
	next.Epoch = cur.Epoch + 1
	next.CreatedAt = time.Now().UTC()
	token, err := valiss.IssueOperator(opKP, valiss.WithName(cur.Name), valiss.WithEpoch(next.Epoch))
	if err != nil {
		return rotationResult{}, fmt.Errorf("valiss: re-minting operator token: %w", err)
	}
	next.Token = token
	if err := st.PutEntity(next); err != nil {
		return rotationResult{}, err
	}

	result := rotationResult{operator: next}
	accounts, err := st.ListChildren(store.KindAccount, cur.Path)
	if err != nil {
		return rotationResult{}, err
	}
	for _, acct := range accounts {
		users, err := reissueAccountAtEpoch(st, opKP, acct, next.Epoch)
		if err != nil {
			return rotationResult{}, err
		}
		result.accounts++
		result.users += users
	}

	if err := st.Append(store.AuditEntry{Op: store.AuditOperatorRotate, Path: cur.Path,
		Detail: fmt.Sprintf("epoch %d -> %d, re-issued %d account(s) and %d user(s)",
			cur.Epoch, next.Epoch, result.accounts, result.users)}); err != nil {
		return rotationResult{}, err
	}
	return result, nil
}

// reissueAccountAtEpoch re-mints an account token (and its users' tokens) at the
// new epoch, keeping every key. It swaps the account's allowlist entry from the
// old jti to the new one when the account was allowlisted, marks the superseded
// account issuance revoked, records the new account issuance, and re-mints each
// live user token beneath it. It returns the number of users re-issued.
func reissueAccountAtEpoch(st *store.Local, opKP nkeys.KeyPair, acct store.EntityRecord, epoch uint64) (int, error) {
	oldClaims, err := valiss.Decode(acct.Token)
	if err != nil {
		return 0, fmt.Errorf("valiss: decoding account token: %w", err)
	}
	newToken, err := valiss.IssueAccount(opKP, acct.PublicKey, valiss.WithName(acct.Name), valiss.WithEpoch(epoch))
	if err != nil {
		return 0, fmt.Errorf("valiss: re-minting account token: %w", err)
	}
	next := acct
	next.Generation = acct.Generation + 1
	next.Epoch = epoch
	next.Token = newToken
	next.CreatedAt = time.Now().UTC()
	if err := st.PutEntity(next); err != nil {
		return 0, err
	}
	// Supersede the old account issuance and record the new one.
	if err := st.RevokeToken(oldClaims.ID, time.Now().UTC()); err != nil {
		return 0, err
	}
	if err := recordAccountIssuance(st, next); err != nil {
		return 0, err
	}
	// Preserve allowlist membership across the jti change.
	wasAllowed, err := st.AllowlistContains(oldClaims.ID)
	if err != nil {
		return 0, err
	}
	if wasAllowed {
		newClaims, err := valiss.Decode(newToken)
		if err != nil {
			return 0, fmt.Errorf("valiss: decoding re-minted account token: %w", err)
		}
		if _, err := st.RemoveAllowlist(oldClaims.ID); err != nil {
			return 0, err
		}
		if _, err := st.AddAllowlist(newClaims.ID, time.Now().UTC()); err != nil {
			return 0, err
		}
	}

	acctKP, err := nkeys.FromSeed(acct.Seed)
	if err != nil {
		return 0, fmt.Errorf("valiss: loading account key: %w", err)
	}
	users, err := st.ListChildren(store.KindUser, acct.Path)
	if err != nil {
		return 0, err
	}
	for _, user := range users {
		userToken, err := valiss.IssueUser(acctKP, user.PublicKey, valiss.WithName(user.Name), valiss.WithEpoch(epoch))
		if err != nil {
			return 0, fmt.Errorf("valiss: re-minting user token: %w", err)
		}
		nextUser := user
		nextUser.Generation = user.Generation + 1
		nextUser.Epoch = epoch
		nextUser.Token = userToken
		nextUser.CreatedAt = time.Now().UTC()
		if err := st.PutEntity(nextUser); err != nil {
			return 0, err
		}
	}
	return len(users), nil
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
