// platform_handlers.go serves the platform plane (A3.1, A3.7). These routes
// name no tenant and reach no tenant object; they authorize against the
// platform singleton through authz.CheckPlatform rather than through
// authorizeTenant, and they sit behind httpx.PlatformOnly rather than
// httpx.TenantMatch (ADR-111).
package main

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/organization"
)

// authorizePlatform is authorizeTenant's platform twin: one check of 6.1's
// platform form against platform:main, before the handler does anything.
//
// A denial is 403 forbidden. There is no tenant_mismatch equivalent here,
// because there is no tenant for the caller to have named wrongly — the
// plane itself is checked by PlatformOnly at the edge and again inside
// CheckPlatform.
func authorizePlatform(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger, relation string,
) bool {
	principal, ok := httpx.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, httpx.CodeInternal, "authorization ran before authentication")
		return false
	}

	allowed, err := authz.CheckPlatform(r.Context(), pool, fga, authz.PlatformRequest{
		Principal: principal,
		Relation:  relation,
		Action:    r.Method + " " + r.URL.Path,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	})
	if err != nil {
		logger.ErrorContext(r.Context(), "platform authorization failed",
			"error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
		httpx.WriteError(w, httpx.CodeInternal, "internal error")
		return false
	}
	if !allowed {
		httpx.WriteError(w, httpx.CodeForbidden, "forbidden")
		return false
	}
	return true
}

// handleCreateTenant is A3.7's POST /platform/tenants. It provisions a
// tenant and its first owner together (ADR-113) — see
// organization.CreateTenant for why those cannot be two calls.
func handleCreateTenant(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizePlatform(w, r, pool, fga, logger, "may_provision_tenant") {
			return
		}

		var body struct {
			LegalName    string `json:"legal_name"`
			DisplayName  string `json:"display_name"`
			Jurisdiction string `json:"jurisdiction"`
			Owner        struct {
				DisplayName string `json:"display_name"`
				IDPSubject  string `json:"idp_subject"`
			} `json:"owner"`
		}
		if !readBody(w, r, &body) {
			return
		}

		// A3.4: each message names the field the caller omitted, which is
		// their own input and discloses nothing.
		for _, f := range []struct{ name, value string }{
			{"legal_name", body.LegalName},
			{"display_name", body.DisplayName},
			{"jurisdiction", body.Jurisdiction},
			{"owner.idp_subject", body.Owner.IDPSubject},
		} {
			if f.value == "" {
				httpx.WriteError(w, httpx.CodeInvalidRequest, f.name+" is required")
				return
			}
		}

		// The owner's display name falls back to the association's: a person
		// whose name nobody supplied is still a person, and a NULL here would
		// make the first administrator harder to identify than the tenant
		// they administer.
		ownerName := body.Owner.DisplayName
		if ownerName == "" {
			ownerName = body.DisplayName + " (owner)"
		}

		var tenant organization.Tenant
		// db.InTx, not InTenantTx: no tenant context exists yet. Every table
		// this touches is unpoliced (A2.1).
		err := pool.InTx(r.Context(), func(tx pgx.Tx) error {
			var err error
			tenant, err = organization.CreateTenant(r.Context(), tx, organization.NewTenant{
				LegalName:        body.LegalName,
				DisplayName:      body.DisplayName,
				Jurisdiction:     body.Jurisdiction,
				OwnerDisplayName: ownerName,
				OwnerIDPSubject:  body.Owner.IDPSubject,
			})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, tenant)
	}
}
