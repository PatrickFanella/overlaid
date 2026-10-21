package appvault

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/tenancy"
	"git.subcult.tv/PatrickFanella/over-laid/internal/tenantdb"
	"git.subcult.tv/PatrickFanella/over-laid/internal/twitchconduit"
)

var ErrIssuanceUncertain = errors.New("service app issuance unresolved; do not repeat")

// Issuance is fixed administrative metadata. Started and uncertain states never
// grant another attempt. Missing records cannot be recreated through Issue.
type Issuance struct {
	ID                string    `json:"id"`
	ClientID          string    `json:"clientId"`
	ExpectedRevision  int64     `json:"expectedRevision"`
	State             string    `json:"state"`
	Failure           string    `json:"failure,omitempty"`
	InstalledAppID    string    `json:"installedAppId,omitempty"`
	InstalledRevision int64     `json:"installedRevision,omitempty"`
	Principal         string    `json:"principal"`
	CreatedAt         time.Time `json:"createdAt"`
	DeadlineAt        time.Time `json:"deadlineAt"`
}

const issuanceColumns = `id,client_id,expected_revision,state,coalesce(failure_code,''),coalesce(installed_app_id::text,''),coalesce(installed_revision,0),principal,created_at,deadline_at`

func scanIssuance(row interface{ Scan(...any) error }) (Issuance, error) {
	var r Issuance
	err := row.Scan(&r.ID, &r.ClientID, &r.ExpectedRevision, &r.State, &r.Failure, &r.InstalledAppID, &r.InstalledRevision, &r.Principal, &r.CreatedAt, &r.DeadlineAt)
	return r, err
}
func issuanceSecret(client, secret string) string {
	sum := sha256.Sum256([]byte("overlaid:service-app-issuance-secret:v1\x00" + client + "\x00" + secret))
	return hex.EncodeToString(sum[:])
}
func validIssuanceInput(client, secret string, expected int64) bool {
	if expected < 0 || expected == math.MaxInt64 || secret == "" || len(secret) > 4096 {
		return false
	}
	for _, b := range []byte(secret) {
		if b < 33 || b > 126 {
			return false
		}
	}
	_, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: client, ClientSecret: func(context.Context) (string, error) { return secret, nil }})
	return err == nil
}
func checkIssuanceRevision(ctx context.Context, tx *sql.Tx, client string, expected int64) error {
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM service_app_credentials WHERE provider='twitch' AND client_id=$1`, client).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) && expected == 0 {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || err == nil && revision != expected {
		return ErrRevision
	}
	return err
}

// PrepareIssuance creates no credential and makes no provider request. IDs and
// original deadlines are database-generated; there is no caller-ID upsert.
func (s *Store) PrepareIssuance(ctx context.Context, client, secret string, expected int64) (Issuance, error) {
	if !validIssuanceInput(client, secret, expected) {
		return Issuance{}, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Issuance{}, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	// Expiry never refreshes a deadline or releases an attempted request for replay.
	if _, err = tx.ExecContext(bound, `UPDATE service_app_issuance SET state=CASE WHEN state='prepared' THEN 'refused' ELSE 'uncertain' END,failure_code=CASE WHEN state='prepared' THEN 'expired' ELSE 'uncertain' END,completed_at=clock_timestamp() WHERE state IN('prepared','started') AND deadline_at<=clock_timestamp()`); err != nil {
		return Issuance{}, failure(bound, err)
	}
	if _, err = tx.ExecContext(bound, `DELETE FROM service_app_issuance WHERE state IN('issued','refused') AND created_at<clock_timestamp()-interval '24 hours'`); err != nil {
		return Issuance{}, failure(bound, err)
	}
	// Unresolved ownership survives retention. Only a separately reviewed
	// operator resolution may ever release this client for another issuance.
	unresolved, unresolvedErr := scanIssuance(tx.QueryRowContext(bound, `SELECT `+issuanceColumns+` FROM service_app_issuance WHERE client_id=$1 AND state IN('started','uncertain') ORDER BY created_at DESC LIMIT 1`, client))
	if unresolvedErr == nil {
		if err = tx.Commit(); err != nil {
			return Issuance{}, failure(bound, err)
		}
		return unresolved, ErrIssuanceUncertain
	}
	if !errors.Is(unresolvedErr, sql.ErrNoRows) {
		return Issuance{}, failure(bound, unresolvedErr)
	}
	fingerprint := issuanceSecret(client, secret)
	var existingFingerprint string
	existing, err := scanIssuance(tx.QueryRowContext(bound, `SELECT `+issuanceColumns+` FROM service_app_issuance WHERE client_id=$1 AND state IN('prepared','started')`, client))
	if err == nil {
		if err = tx.QueryRowContext(bound, `SELECT secret_fingerprint FROM service_app_issuance WHERE id=$1`, existing.ID).Scan(&existingFingerprint); err != nil {
			return Issuance{}, failure(bound, err)
		}
		if existing.ExpectedRevision != expected || subtle.ConstantTimeCompare([]byte(fingerprint), []byte(existingFingerprint)) != 1 {
			return Issuance{}, ErrRevision
		}
		if err = tx.Commit(); err != nil {
			return Issuance{}, failure(bound, err)
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Issuance{}, failure(bound, err)
	}
	if err = checkIssuanceRevision(bound, tx, client, expected); err != nil {
		return Issuance{}, failure(bound, err)
	}
	var count, clients int
	if err = tx.QueryRowContext(bound, `SELECT (SELECT count(*) FROM service_app_issuance),(SELECT count(*) FROM(SELECT client_id FROM service_app_credentials UNION SELECT client_id FROM service_app_issuance WHERE state IN('prepared','started','uncertain') UNION SELECT $1::text) reserved)`, client).Scan(&count, &clients); err != nil {
		return Issuance{}, failure(bound, err)
	}
	if count >= 1000 || clients > 8 {
		return Issuance{}, ErrCapacity
	}
	r, err := scanIssuance(tx.QueryRowContext(bound, `INSERT INTO service_app_issuance(client_id,expected_revision,secret_fingerprint) VALUES($1,$2,$3) RETURNING `+issuanceColumns, client, expected, fingerprint))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Issuance{}, failure(bound, err)
	}
	return r, nil
}

// LookupIssuance is metadata-only and read-only, including after expiry/restart.
func (s *Store) LookupIssuance(ctx context.Context, id string) (Issuance, error) {
	if !tenantdb.ValidID(id) || id != strings.ToLower(id) {
		return Issuance{}, ErrInvalid
	}
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Validate(bound); err != nil {
		return Issuance{}, err
	}
	r, err := scanIssuance(s.db.QueryRowContext(bound, `SELECT `+issuanceColumns+` FROM service_app_issuance WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Issuance{}, ErrRevision
	}
	return r, failure(bound, err)
}

func claimDigest(id string, claim []byte) string {
	sum := sha256.Sum256(append([]byte("overlaid:service-app-issuance-claim:v1\x00"+id+"\x00"), claim...))
	return hex.EncodeToString(sum[:])
}

// Issue makes one provider attempt only after committing a one-use claim.
// Successful installation and its issuance result commit together. The caller
// must retain the prepared ID and reconcile uncertainty, never mint a new retry.
func (s *Store) Issue(ctx context.Context, k tenancy.Keyring, id, client, secret string, expected int64, httpClient *http.Client) (Issuance, error) {
	if !tenantdb.ValidID(id) || id != strings.ToLower(id) || !validIssuanceInput(client, secret, expected) || len(k.Active) < 1 || len(k.Active) > 40 || len(k.Keys[k.Active]) != 32 {
		return Issuance{}, ErrInvalid
	}
	// Freeze the one sealing version before admission/provider work.
	k = tenancy.Keyring{Active: k.Active, Keys: map[string][]byte{k.Active: append([]byte(nil), k.Keys[k.Active]...)}}
	bound, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	authorizer, err := twitchconduit.NewAppAuthorizer(twitchconduit.AppAuthorizationConfig{ClientID: client, ClientSecret: func(call context.Context) (string, error) {
		if call.Err() != nil {
			return "", call.Err()
		}
		return secret, nil
	}, HTTPClient: httpClient})
	if err != nil {
		return Issuance{}, ErrInvalid
	}
	var claim [32]byte
	if _, err = rand.Read(claim[:]); err != nil {
		return Issuance{}, ErrUnavailable
	}
	digest := claimDigest(id, claim[:])
	r, claimed, err := s.claimIssuance(bound, id, client, secret, expected, digest)
	if err != nil || !claimed {
		if err == nil && r.State != "issued" {
			err = ErrUnavailable
		}
		return r, err
	}
	acquired, providerErr := authorizer.Acquire(bound)
	if providerErr == nil && acquired.Outcome == twitchconduit.AcquisitionIssued && acquired.Credential != nil && bound.Err() == nil {
		installed, installErr := s.installIssuance(bound, k, r, digest, secret, *acquired.Credential)
		if installErr == nil {
			return installed, nil
		}
	}
	state, code := "uncertain", "uncertain"
	if acquired.Outcome == twitchconduit.AcquisitionRefused {
		state, code = "refused", "provider_refused"
	}
	if acquired.Outcome == twitchconduit.AcquisitionNotStarted {
		state, code = "refused", "not_started"
	}
	// Claim/issuance uncertainty survives caller cancellation and process restarts.
	finished, finishErr := s.finishIssuance(r, digest, state, code)
	if finishErr == nil {
		r = finished
	}
	if state == "uncertain" || finishErr != nil {
		return r, errors.Join(ErrIssuanceUncertain, bound.Err())
	}
	if bound.Err() != nil {
		return r, bound.Err()
	}
	return r, ErrUnavailable
}

func (s *Store) claimIssuance(ctx context.Context, id, client, secret string, expected int64, digest string) (Issuance, bool, error) {
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Issuance{}, false, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	r, err := scanIssuance(tx.QueryRowContext(bound, `SELECT `+issuanceColumns+` FROM service_app_issuance WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Issuance{}, false, ErrRevision
	}
	if err != nil {
		return Issuance{}, false, failure(bound, err)
	}
	var fingerprint string
	if err = tx.QueryRowContext(bound, `SELECT secret_fingerprint FROM service_app_issuance WHERE id=$1`, id).Scan(&fingerprint); err != nil {
		return Issuance{}, false, failure(bound, err)
	}
	if r.ClientID != client || r.ExpectedRevision != expected || subtle.ConstantTimeCompare([]byte(fingerprint), []byte(issuanceSecret(client, secret))) != 1 {
		return Issuance{}, false, ErrRevision
	}
	if r.State != "prepared" {
		if r.State == "started" || r.State == "uncertain" {
			return r, false, ErrIssuanceUncertain
		}
		if r.State == "refused" {
			return r, false, ErrUnavailable
		}
		return r, false, nil
	}
	var eligible bool
	if err = tx.QueryRowContext(bound, `SELECT deadline_at>clock_timestamp() FROM service_app_issuance WHERE id=$1`, id).Scan(&eligible); err != nil {
		return Issuance{}, false, failure(bound, err)
	}
	if !eligible {
		r, err = scanIssuance(tx.QueryRowContext(bound, `UPDATE service_app_issuance SET state='refused',failure_code='expired',completed_at=clock_timestamp() WHERE id=$1 RETURNING `+issuanceColumns, id))
		if err == nil {
			err = tx.Commit()
		}
		return r, false, failure(bound, err)
	}
	if err = checkIssuanceRevision(bound, tx, client, expected); err != nil {
		return r, false, failure(bound, err)
	}
	r, err = scanIssuance(tx.QueryRowContext(bound, `UPDATE service_app_issuance SET state='started',claim_hash=$2,started_at=clock_timestamp() WHERE id=$1 AND state='prepared' AND deadline_at>clock_timestamp() RETURNING `+issuanceColumns, id, digest))
	if err != nil {
		return Issuance{}, false, failure(bound, err)
	}
	if err = tx.Commit(); err != nil {
		return r, false, errors.Join(ErrIssuanceUncertain, bound.Err())
	}
	return r, true, nil
}

func (s *Store) installIssuance(ctx context.Context, k tenancy.Keyring, r Issuance, digest, secret string, credential twitchconduit.AppCredential) (Issuance, error) {
	bound, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.transaction(bound)
	if err != nil {
		return Issuance{}, failure(bound, err)
	}
	defer func() { _ = tx.Rollback() }()
	var valid bool
	if err = tx.QueryRowContext(bound, `SELECT EXISTS(SELECT 1 FROM service_app_issuance WHERE id=$1 AND client_id=$2 AND expected_revision=$3 AND state='started' AND claim_hash=$4 AND deadline_at>clock_timestamp())`, r.ID, r.ClientID, r.ExpectedRevision, digest).Scan(&valid); err != nil || !valid {
		return Issuance{}, ErrRevision
	}
	m, err := installTx(bound, tx, k, secret, credential, r.ExpectedRevision)
	if err != nil {
		return Issuance{}, failure(bound, err)
	}
	r, err = scanIssuance(tx.QueryRowContext(bound, `UPDATE service_app_issuance SET state='issued',completed_at=clock_timestamp(),installed_app_id=$3,installed_revision=$4 WHERE id=$1 AND state='started' AND claim_hash=$2 AND deadline_at>clock_timestamp() RETURNING `+issuanceColumns, r.ID, digest, m.ID, m.Revision))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Issuance{}, failure(bound, err)
	}
	return r, nil
}

func (s *Store) finishIssuance(r Issuance, digest, state, code string) (Issuance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.transaction(ctx)
	if err != nil {
		return Issuance{}, failure(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	r, err = scanIssuance(tx.QueryRowContext(ctx, `UPDATE service_app_issuance SET state=$3,failure_code=$4,completed_at=clock_timestamp() WHERE id=$1 AND state='started' AND claim_hash=$2 RETURNING `+issuanceColumns, r.ID, digest, state, code))
	if err == nil {
		err = tx.Commit()
	}
	return r, failure(ctx, err)
}
