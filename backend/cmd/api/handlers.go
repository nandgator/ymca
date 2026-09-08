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
	"errors"
	"log/slog"
	"net/http"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
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
	principal, ok := httpx.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeInternal, "no principal on request context")
		return false
	}

	allowed, err := authz.Check(r.Context(), pool, fga, authz.Request{
		Principal:       principal,
		RequestTenantID: r.PathValue("tenant"),
		Object:          object,
		Relation:        relation,
		Action:          object.Type + "." + relation,
		RequestID:       httpx.RequestIDFromContext(r.Context()),
	})
	switch {
	case errors.Is(err, authz.ErrTenantMismatch):
		httpx.WriteError(w, httpx.CodeTenantMismatch, "that object is not in this tenant")
		return false
	case err != nil:
		logger.ErrorContext(r.Context(), "authorization check failed",
			"error", err, "relation", relation, "object_type", object.Type,
			"request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
		return false
	case !allowed:
		httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
		return false
	}
	return true
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

// writeDomainError maps a domain error to A3.4's vocabulary. Anything not
// recognised is a 500 with no detail: A3.4 forbids leaking SQL or an
// identifier the caller could not already name.
func writeDomainError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, membership.ErrNotFound),
		errors.Is(err, consumption.ErrNotFound),
		errors.Is(err, organization.ErrNotFound),
		// Normally unreachable: the `admin` check on the named parent
		// refuses a parent this tenant cannot see before any row is
		// written. It survives as a 404 for the case authorization cannot
		// cover — a parent removed between the check and the insert.
		errors.Is(err, organization.ErrEdgeForeignEndpoint):
		httpx.WriteError(w, httpx.CodeNotFound, "not found")
	case errors.Is(err, page.ErrInvalidCursor), errors.Is(err, page.ErrInvalidLimit),
		errors.Is(err, membership.ErrInvalidEntitledType),
		errors.Is(err, organization.ErrOwnerSubjectTaken),
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
	default:
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
