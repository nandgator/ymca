// config_handlers.go is A3.7's configuration surface: entitlement bundles,
// what they entitle, membership plans, and consumption types.
//
// It exists because the slice could not run without it. membership.plan_id is
// NOT NULL, ADR-107's covered_member tuple needs a plan to point at, and
// consumption_type.may_record resolves through `entitled` from a bundle —
// so with no way to create any of the three, a member admitted through this
// API would hold no entitlements and could not record a meal.
//
// Every handler here follows the same four steps, in this order:
//
//	1  authorize      one 6.1 check against the tenant (ADR-104 for lists)
//	2  read           decode the body, or the page parameters
//	3  act            one tenant transaction, domain package does the work
//	4  respond        A3.4's shape on failure, A3.5's on a list
package main

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/organization"
	"github.com/nandgator/ymca/backend/internal/page"
)

// configurePermission is the relation every endpoint in this file checks.
// Configuration is administration: 05.3.2 makes plans a commercial and
// sometimes constitutional artifact, and ADR-076 marks them
// tenant-configurable, which is authority over the tenant rather than over
// any one object.
const configurePermission = "admin"

// authorizeTenant runs 6.1 against the tenant named in the path and writes
// A3.4's response on refusal. It reports whether the caller may proceed.
//
// A denial is 403 forbidden; ErrTenantMismatch cannot reach here, because
// TenantMatch already ran on the route.
func authorizeTenant(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger, relation string,
) bool {
	tenantID := r.PathValue("tenant")
	principal, ok := httpx.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeInternal, "no principal on request context")
		return false
	}

	allowed, err := authz.Check(r.Context(), pool, fga, authz.Request{
		Principal:       principal,
		RequestTenantID: tenantID,
		Object:          authz.Object{Type: "tenant", ID: tenantID, TenantID: tenantID},
		Relation:        relation,
		Action:          "tenant." + relation,
		RequestID:       httpx.RequestIDFromContext(r.Context()),
	})
	if err != nil {
		logger.ErrorContext(r.Context(), "authorization check failed",
			"error", err, "relation", relation,
			"request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
		return false
	}
	if !allowed {
		httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
		return false
	}
	return true
}

// writeDomainError maps a domain error to A3.4's vocabulary. Anything not
// recognised is a 500 with no detail: A3.4 forbids leaking SQL or an
// identifier the caller could not already name.
func writeDomainError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, membership.ErrNotFound), errors.Is(err, consumption.ErrNotFound):
		httpx.WriteError(w, httpx.CodeNotFound, "not found")
	case errors.Is(err, page.ErrInvalidCursor), errors.Is(err, page.ErrInvalidLimit),
		errors.Is(err, membership.ErrInvalidEntitledType),
		errors.Is(err, organization.ErrOwnerSubjectTaken):
		// A3.4: these name a value the caller supplied, so echoing it
		// discloses nothing they did not already send.
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

func handleCreateBundle(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.Name == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "name is required")
			return
		}

		tenantID := r.PathValue("tenant")
		var bundle membership.Bundle
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			bundle, err = membership.CreateBundle(r.Context(), tx, tenantID, body.Name)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, bundle)
	}
}

func handleListBundles(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		var rows []membership.Bundle
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = membership.ListBundles(r.Context(), tx, tenantID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, membership.BundleKey))
	}
}

func handleEntitle(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		var body struct {
			ObjectType string `json:"object_type"`
			ObjectID   string `json:"object_id"`
		}
		if !readBody(w, r, &body) {
			return
		}

		tenantID := r.PathValue("tenant")
		bundleID := r.PathValue("bundle")
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			return membership.Entitle(r.Context(), tx, tenantID, bundleID,
				body.ObjectType, body.ObjectID)
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleCreatePlan(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		var body struct {
			Code        string  `json:"code"`
			Name        string  `json:"name"`
			Acquisition string  `json:"acquisition"`
			Duration    string  `json:"duration"`
			BundleID    *string `json:"entitlement_bundle_id"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.Code == "" || body.Name == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "code and name are required")
			return
		}

		tenantID := r.PathValue("tenant")
		var plan membership.Plan
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			plan, err = membership.CreatePlan(r.Context(), tx, tenantID, membership.Plan{
				Code:        body.Code,
				Name:        body.Name,
				Acquisition: body.Acquisition,
				Duration:    body.Duration,
				BundleID:    body.BundleID,
			})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, plan)
	}
}

func handleListPlans(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		var rows []membership.Plan
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = membership.ListPlans(r.Context(), tx, tenantID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, membership.PlanKey))
	}
}

func handleCreateConsumptionType(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		var body struct {
			Name       string  `json:"name"`
			Obligates  bool    `json:"obligates"`
			Recurrence *string `json:"recurrence"`
			RecordMode string  `json:"record_mode"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.RecordMode == "" {
			body.RecordMode = "EITHER"
		}

		tenantID := r.PathValue("tenant")
		var created consumption.Type
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			created, err = consumption.CreateType(r.Context(), tx, tenantID, consumption.Type{
				Name:       body.Name,
				Obligates:  body.Obligates,
				Recurrence: body.Recurrence,
				RecordMode: body.RecordMode,
			})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	}
}

func handleListConsumptionTypes(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, configurePermission) {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		var rows []consumption.Type
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = consumption.ListTypes(r.Context(), tx, tenantID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, consumption.TypeKey))
	}
}
