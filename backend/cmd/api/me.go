// me.go is GET /api/v1/t/{tenant}/me (D6, A3.7). It is small enough to live
// beside the wiring in main.go rather than its own package — there is no
// other handler in this step to share a package with.
package main

import (
	"log/slog"
	"net/http"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
)

// tenantPermissions is D6's candidate set: exactly four, each a full
// authz.Check against the tenant object named in the path. Only the ones
// that pass are reported. This is not a general-purpose scan of every
// relation the tenant type declares — 05.1's FGA model separately grants
// jit_grantee and administered_by, which are deliberately not tenant-plane
// permissions and so are not asked about here.
var tenantPermissions = []string{"admin", "member", "finance_reader", "safeguarding_reader"}

type meResponse struct {
	PrincipalID string   `json:"principal_id"`
	Kind        string   `json:"kind"`
	PersonID    string   `json:"person_id"`
	TenantID    string   `json:"tenant_id"`
	Permissions []string `json:"permissions"`
}

func handleMe(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.PathValue("tenant")

		principal, ok := httpx.PrincipalFromContext(r.Context())
		if !ok {
			// TenantMatch runs before this handler on this route and
			// requires the same thing; reaching here without a principal
			// is a wiring defect, not a caller error.
			httpx.WriteError(w, httpx.CodeInternal, "no principal on request context")
			return
		}
		requestID := httpx.RequestIDFromContext(r.Context())

		granted := []string{}
		for _, relation := range tenantPermissions {
			allowed, err := authz.Check(r.Context(), pool, fga, authz.Request{
				Principal:       principal,
				RequestTenantID: tenantID,
				Object:          authz.Object{Type: "tenant", ID: tenantID, TenantID: tenantID},
				Relation:        relation,
				Action:          "tenant." + relation,
				RequestID:       requestID,
			})
			if err != nil {
				logger.ErrorContext(r.Context(), "authz check failed",
					"error", err, "relation", relation, "request_id", requestID)
				httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
				return
			}
			if allowed {
				granted = append(granted, "tenant:"+relation)
			}
		}

		httpx.WriteJSON(w, http.StatusOK, meResponse{
			PrincipalID: principal.ID,
			Kind:        string(principal.Kind),
			PersonID:    principal.PersonID,
			TenantID:    tenantID,
			Permissions: granted,
		})
	}
}
