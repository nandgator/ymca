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
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/page"
)

// configurePermission is the relation every endpoint in this file checks.
// Configuration is administration: 05.3.2 makes plans a commercial and
// sometimes constitutional artifact, and ADR-076 marks them
// tenant-configurable, which is authority over the tenant rather than over
// any one object.
//
// The check itself, the error mapping and body decoding are in handlers.go,
// shared with the organization endpoints.
const configurePermission = "admin"

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
