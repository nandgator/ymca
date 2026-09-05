// Package idempotency implements A3.6: a client-supplied key makes a POST
// safe to retry, because the result is stored and replayed rather than the
// work being done twice.
//
// The whole of the design is that the key is recorded INSIDE the transaction
// that does the work (A2.10). Effect and record commit together or not at
// all, which removes the state every other design of this has to reason
// about — there is no "in flight" row, no reservation to time out, and no
// window in which a crash leaves a key claiming work that never happened.
//
// It is deliberately transport-neutral. Nothing here mentions HTTP: a CLI
// admitting a member or recording consumption needs exactly the same
// guarantee, and REVIEW.md B7 says decisions must account for that caller.
// The HTTP header is read in internal/httpx and passed in as a Key.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Key identifies one attempt at one operation. Endpoint scopes the key so
// that a client reusing a key across two different operations gets two
// operations rather than a replay of the wrong one.
type Key struct {
	TenantID    string
	Endpoint    string
	Key         string
	PrincipalID string
	// RequestDigest is Digest(body). A replay carrying a different digest is
	// refused rather than answered.
	RequestDigest string
}

// Result is what a completed operation returned, and what a replay returns.
type Result struct {
	StatusCode int
	Body       json.RawMessage
	// Replayed is true when this came from the store rather than from
	// running the operation. Callers surface it; nothing here depends on it.
	Replayed bool
}

// ErrKeyReused is A3.4's 409: the same key arrived with a different body.
// Replaying the first response would silently discard the second request,
// which is the failure this mechanism exists to prevent, inverted.
var ErrKeyReused = errors.New("idempotency: key reused with a different request")

// Digest is the request_digest of a body.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// uniqueViolation is the SQLSTATE for a primary key collision, which here
// means a concurrent identical request committed first.
const uniqueViolation = "23505"

// Do runs op inside tx and records key with the result, so that a later
// attempt with the same key replays instead of repeating.
//
// The caller supplies the transaction, because the whole guarantee rests on
// op's writes and this record sharing one. Do must never open its own.
//
// The concurrent case resolves itself without a lock: two identical requests
// both run op, the first to commit wins the primary key, and the second sees
// a unique violation. Its transaction is then poisoned — pgx will reject
// every further statement on it — so it cannot read the winner's row itself.
// It returns ErrConcurrent, and the caller retries the whole thing in a fresh
// transaction, where Lookup finds the committed row.
func Do(
	ctx context.Context,
	tx pgx.Tx,
	key Key,
	op func(context.Context, pgx.Tx) (Result, error),
) (Result, error) {
	result, err := op(ctx, tx)
	if err != nil {
		return Result{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_key
		    (tenant_id, endpoint, key, request_digest, principal_id,
		     status_code, response_body)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, key.TenantID, key.Endpoint, key.Key, key.RequestDigest,
		key.PrincipalID, result.StatusCode, []byte(result.Body))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Result{}, ErrConcurrent
		}
		return Result{}, fmt.Errorf("idempotency: record key: %w", err)
	}
	return result, nil
}

// ErrConcurrent means an identical request committed while this one was
// running. It is not a failure of either request: the caller retries in a
// fresh transaction and Lookup replays the winner's response.
var ErrConcurrent = errors.New("idempotency: a concurrent request committed first")

// Lookup returns the stored result for a key, if the operation already ran.
//
// A row whose request_digest differs from key.RequestDigest yields
// ErrKeyReused: same key, different request. That is the one case where this
// mechanism must refuse rather than replay.
func Lookup(ctx context.Context, tx pgx.Tx, key Key) (Result, bool, error) {
	var (
		statusCode int
		body       []byte
		digest     string
	)
	err := tx.QueryRow(ctx, `
		SELECT status_code, response_body, request_digest
		FROM idempotency_key
		WHERE tenant_id = $1 AND endpoint = $2 AND key = $3
	`, key.TenantID, key.Endpoint, key.Key).Scan(&statusCode, &body, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("idempotency: look up key: %w", err)
	}
	if digest != key.RequestDigest {
		return Result{}, false, ErrKeyReused
	}
	return Result{StatusCode: statusCode, Body: body, Replayed: true}, true, nil
}
