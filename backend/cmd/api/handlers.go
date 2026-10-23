// handlers.go holds what every A3.7 handler needs: the 6.1 check, the
// mapping from a domain error to A3.4's vocabulary, and body decoding.
//
// These lived in config_handlers.go while configuration was the only
// surface. They are here now because three files use them, and because
// authorizeObject below is the general form — 8.2 could assume the object
// was always the tenant, and `POST /t/{t}/units` is the first endpoint for
// which that is false.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/finance"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/idempotency"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/organization"
	"github.com/nandgator/ymca/backend/internal/page"
)

// authorizeObject runs 6.1 against one object and writes A3.4's response on
// refusal. It reports whether the caller may proceed.
//
// Two kinds of refusal reach a caller here and they are deliberately
// different codes. A graph or validity DENY is 403 `forbidden`. Step 1's
// tenant mismatch is 403 `tenant_mismatch` — ADR-105 gives it its own code
// because refusing a tenant the caller named themselves discloses nothing,
// and because that DENY is the one that evidences somebody probing for
// tenants they cannot reach. It is audited before it gets here.
func authorizeObject(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger,
	object authz.Object, relation string,
) bool {
	allowed, proceed := checkObject(w, r, pool, fga, logger, object, relation, "")
	if !proceed {
		return false
	}
	if !allowed {
		httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
		return false
	}
	return true
}

// checkObject is 6.1 for one object, reporting the decision rather than
// acting on a denial. proceed is false when a response has already been
// written — a tenant mismatch or a failure of the check itself — and the
// caller must stop; otherwise allowed is the answer, and what a denial means
// is the caller's to say. alsoPerson is authz.Request.AlsoAsPerson.
//
// authorizeObject is the common case, a denial that is simply a 403.
// A correction past its window is the other: its denial is a 409 the caller
// can act on (A3.10), which is why the split exists.
func checkObject(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger,
	object authz.Object, relation, alsoPerson string,
) (allowed, proceed bool) {
	principal, ok := httpx.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeInternal, "no principal on request context")
		return false, false
	}

	allowed, err := authz.Check(r.Context(), pool, fga, authz.Request{
		Principal:       principal,
		RequestTenantID: r.PathValue("tenant"),
		Object:          object,
		Relation:        relation,
		AlsoAsPerson:    alsoPerson,
		Action:          object.Type + "." + relation,
		RequestID:       httpx.RequestIDFromContext(r.Context()),
	})
	switch {
	case errors.Is(err, authz.ErrTenantMismatch):
		httpx.WriteError(w, httpx.CodeTenantMismatch, "that object is not in this tenant")
		return false, false
	case err != nil:
		logger.ErrorContext(r.Context(), "authorization check failed",
			"error", err, "relation", relation, "object_type", object.Type,
			"request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
		return false, false
	}
	return allowed, true
}

// authorizeTenant checks a relation on the tenant named in the path.
func authorizeTenant(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger, relation string,
) bool {
	tenantID := r.PathValue("tenant")
	return authorizeObject(w, r, pool, fga, logger,
		authz.Object{Type: "tenant", ID: tenantID, TenantID: tenantID}, relation)
}

// authorizeUnit checks a relation on a unit named in the path.
//
// Object.TenantID is the REQUEST tenant, and that needs saying because
// authz.Object's own comment asks a caller checking some other object type
// to supply the object's real tenant. There is no generic resolver, and for
// a unit there does not need to be one: `organizational_unit` is under RLS,
// so a unit belonging to another tenant is not readable on this connection
// at all, and it has no `auth_parent` path to this principal either — the
// graph denies it in step 3 rather than step 1. Making step 1 refuse it
// instead would require a lookup that RLS has already made pointless.
//
// The consequence is that a unit id from another tenant reads as 403
// forbidden rather than 403 tenant_mismatch. Both are refusals disclosing
// nothing, and A3.3 wants them indistinguishable.
func authorizeUnit(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger, unitID, relation string,
) bool {
	return authorizeObject(w, r, pool, fga, logger, authz.Object{
		Type:     "organizational_unit",
		ID:       unitID,
		TenantID: r.PathValue("tenant"),
	}, relation)
}

// runIdempotent is A3.6's flow around one operation: look the key up,
// otherwise do the work and record the key in the SAME transaction, so that
// the effect and the record of it commit together.
//
// status is the code a fresh success returns; a replay returns whatever was
// stored, which is normally the same number and is read from the row rather
// than assumed.
//
// Two attempts, not a loop. The only retryable failure is ErrConcurrent,
// which means an identical request won the primary key while this one ran —
// so the winner is committed and the second attempt's Lookup finds it. A
// third attempt could learn nothing the second did not. Two ErrConcurrents in
// a row falls through to writeDomainError, which has no case for it and so
// logs and answers 500: an honest answer to something that should not happen.
func runIdempotent(
	w http.ResponseWriter, r *http.Request, pool *db.DB, logger *slog.Logger,
	key idempotency.Key, status int, op func(context.Context, pgx.Tx) (any, error),
) {
	// A zero key is a programming error in the caller of this function, not
	// a caller of the API, and it must not be allowed to look like one. Its
	// empty tenant reaches PostgreSQL as ''::uuid, which raises 22P02 — the
	// SQLSTATE writeDomainError reads as a malformed identifier — so without
	// this a handler that ignored the error from httpx.IdempotencyKey would
	// answer 400 and blame the client for the server's mistake. Found by
	// removing the key requirement on purpose and watching the suite stay
	// green.
	if key.Key == "" || key.TenantID == "" {
		logger.ErrorContext(r.Context(), "idempotency key not built",
			"endpoint", key.Endpoint, "request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "internal error")
		return
	}

	tenantID := r.PathValue("tenant")
	for attempt := 0; attempt < 2; attempt++ {
		var result idempotency.Result
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			stored, found, err := idempotency.Lookup(r.Context(), tx, key)
			if err != nil {
				return err
			}
			if found {
				result = stored
				return nil
			}
			result, err = idempotency.Do(r.Context(), tx, key,
				func(ctx context.Context, tx pgx.Tx) (idempotency.Result, error) {
					value, opErr := op(ctx, tx)
					if opErr != nil {
						return idempotency.Result{}, opErr
					}
					// The stored body is what the caller will be sent on a
					// replay, so it is encoded here rather than by the
					// handler — one encoding, one thing to be equal to.
					body, encErr := json.Marshal(value)
					if encErr != nil {
						return idempotency.Result{}, fmt.Errorf("api: encode response: %w", encErr)
					}
					return idempotency.Result{StatusCode: status, Body: body}, nil
				})
			return err
		})
		if errors.Is(err, idempotency.ErrConcurrent) && attempt == 0 {
			continue
		}
		if err != nil {
			// A3.6: a failed attempt rolled back, key row included, so
			// there is nothing to replay and a retry starts fresh.
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteStored(w, result.StatusCode, result.Body)
		return
	}
}

// writeDomainError maps a domain error to A3.4's vocabulary. Anything not
// recognised is a 500 with no detail: A3.4 forbids leaking SQL or an
// identifier the caller could not already name.
func writeDomainError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, membership.ErrNotFound),
		errors.Is(err, consumption.ErrNotFound),
		errors.Is(err, finance.ErrNotFound),
		errors.Is(err, organization.ErrNotFound),
		// Normally unreachable: the `admin` check on the named parent
		// refuses a parent this tenant cannot see before any row is
		// written. It survives as a 404 for the case authorization cannot
		// cover — a parent removed between the check and the insert.
		errors.Is(err, organization.ErrEdgeForeignEndpoint):
		httpx.WriteError(w, httpx.CodeNotFound, "not found")
	case errors.Is(err, page.ErrInvalidCursor), errors.Is(err, page.ErrInvalidLimit),
		errors.Is(err, membership.ErrInvalidEntitledType),
		errors.Is(err, membership.ErrPlanNotOpen),
		errors.Is(err, membership.ErrInvalidReason),
		errors.Is(err, consumption.ErrInactiveType),
		errors.Is(err, consumption.ErrFutureDate),
		errors.Is(err, consumption.ErrUnknownOption),
		errors.Is(err, consumption.ErrAlreadyVoided),
		errors.Is(err, consumption.ErrNotObligating),
		errors.Is(err, consumption.ErrBadPeriod),
		errors.Is(err, consumption.ErrNotAMember),
		errors.Is(err, consumption.ErrInvalidOption),
		errors.Is(err, finance.ErrInvalid),
		errors.Is(err, httpx.ErrMissingIdempotencyKey),
		errors.Is(err, organization.ErrOwnerSubjectTaken),
		errors.Is(err, organization.ErrInvalidTimeZone),
		errors.Is(err, organization.ErrInvalidUnitType),
		errors.Is(err, organization.ErrInvalidParentType):
		// A3.4: these name a value the caller supplied, so echoing it
		// discloses nothing they did not already send.
		httpx.WriteError(w, httpx.CodeInvalidRequest, err.Error())
	case errors.Is(err, organization.ErrEdgeTooDeep), errors.Is(err, organization.ErrEdgeCycle):
		// ADR-016's shape of the graph, not the caller's input — but the
		// caller chose the parent, so the refusal is about their request.
		// D7 has no `conflict` code outside A3.6's idempotency case.
		httpx.WriteError(w, httpx.CodeInvalidRequest, err.Error())
	case errors.Is(err, idempotency.ErrKeyReused):
		// A3.6's one refusal: the same key with a different body. Replaying
		// the first response would silently discard the second request.
		httpx.WriteError(w, httpx.CodeIdempotencyKeyReused,
			"that idempotency key was used for a different request")
	case errors.Is(err, consumption.ErrRecordModeNotAllowed):
		httpx.WriteError(w, httpx.CodeRecordModeNotAllowed, err.Error())
	case errors.Is(err, consumption.ErrAlreadyRecorded):
		httpx.WriteError(w, httpx.CodeAlreadyRecorded, err.Error())
	case errors.Is(err, consumption.ErrAlreadyCorrected):
		httpx.WriteError(w, httpx.CodeRecordAlreadyCorrected, err.Error())
	case errors.Is(err, consumption.ErrWindowClosed):
		httpx.WriteError(w, httpx.CodeWindowClosed, err.Error())
	case errors.Is(err, consumption.ErrPeriodClosed):
		httpx.WriteError(w, httpx.CodePeriodClosed, err.Error())
	case errors.Is(err, consumption.ErrPeriodAlreadyClosed):
		httpx.WriteError(w, httpx.CodePeriodAlreadyClosed, err.Error())
	case errors.Is(err, consumption.ErrObligationOverlaps):
		httpx.WriteError(w, httpx.CodeObligationOverlaps, err.Error())
	case errors.Is(err, consumption.ErrOptionCodeTaken):
		httpx.WriteError(w, httpx.CodeOptionCodeTaken, err.Error())
	case errors.Is(err, finance.ErrRateOverlaps):
		httpx.WriteError(w, httpx.CodeRateOverlaps, err.Error())
	case errors.Is(err, finance.ErrNoRate):
		httpx.WriteError(w, httpx.CodeNoRate, err.Error())
	case errors.Is(err, finance.ErrCurrencyMismatch):
		httpx.WriteError(w, httpx.CodeCurrencyMismatch, err.Error())
	case errors.Is(err, finance.ErrInvoiceState):
		httpx.WriteError(w, httpx.CodeInvoiceWrongState, err.Error())
	case errors.Is(err, finance.ErrInvoiceEmpty):
		httpx.WriteError(w, httpx.CodeInvoiceEmpty, err.Error())
	case errors.Is(err, finance.ErrExceedsBalance):
		httpx.WriteError(w, httpx.CodePaymentExceedsBalance, err.Error())
	case errors.Is(err, membership.ErrWrongState):
		httpx.WriteError(w, httpx.CodeMembershipWrongState, err.Error())
	case errors.Is(err, membership.ErrRevocationFailed):
		// Not a bare "internal error": the caller must be told that the
		// revocation did not happen and that nothing changed (8.3).
		logger.ErrorContext(r.Context(), "revocation failed",
			"error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal,
			"the revocation did not happen and nothing was changed; retry")
	case errors.Is(err, membership.ErrNumberTaken):
		httpx.WriteError(w, httpx.CodeMembershipNumberTaken, err.Error())
	case errors.Is(err, membership.ErrAlreadyActiveMember):
		httpx.WriteError(w, httpx.CodeAlreadyActiveMember, err.Error())
	default:
		// 22P02 is invalid_text_representation: a value the caller sent did
		// not parse as the type its column has, which in this API is always
		// a malformed uuid in a body or a path. Answering 500 would blame
		// the server for the caller's typo. The message names the shape and
		// never echoes the value.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"an identifier in the request is not a well-formed uuid")
			return
		}
		logger.ErrorContext(r.Context(), "request failed",
			"error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "internal error")
	}
}

// readBody decodes a request body, turning every failure into A3.4's
// invalid_request. The message names the problem but never echoes the body.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if _, err := httpx.ReadJSON(r, v); err != nil {
		httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
		return false
	}
	return true
}
