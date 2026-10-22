// membership_handlers.go is A3.7's admission endpoint: `POST /t/{t}/memberships`.
//
// It is the first endpoint in this API that requires an `Idempotency-Key`.
// A3.6 originally scoped the header to "every POST that creates money or a
// consumption record", which admission is not — but the mechanism's own
// rationale, a warden on hostel wifi who retries, applies to it word for
// word. Without a key, a retry after a lost 201 answers 409
// `membership_number_taken` and the caller cannot tell its own success from
// somebody else's collision; with one, the retry replays the original 201
// carrying the membership id. A3.6's scope was widened to say so rather than
// this endpoint quietly doing more than the record describes.
//
// The four steps are the same as every other handler here, with the key read
// between "read" and "act":
//
//	1  authorize      one 6.1 check for tenant:may_approve_membership
//	2  read           the body, then the idempotency key over its raw bytes
//	3  act            one tenant transaction: admit, and record the key
//	4  respond        201 with the membership, or A3.4's shape
package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/page"
)

// admitPermission is the relation admission checks, on the TENANT.
//
// A3.9 makes `may_approve_membership` one of the five candidates /me
// reports, and 05.3.4 puts approval authority at the tenant or at a unit
// above the application, held through an office or a role. This endpoint
// checks the tenant only: ADR-110 makes the relation role-grantable there,
// and the unit-scoped form has no permission declared on
// `organizational_unit` in A1.2 to check against. A unit-scoped admission
// authority is 11.2's, not a silent widening of this one.
//
// It is deliberately NOT `admin`. Admitting members is the one thing a
// front-desk role must be able to do without holding administration of the
// tenant, which is the whole reason ADR-110 made the relation grantable.
const admitPermission = "may_approve_membership"

// admitEndpoint is the A3.6 key's scope: the route pattern, never the
// resolved path. Two admissions differing in their body are different
// operations already, because the digest covers the body.
const admitEndpoint = "POST /t/{t}/memberships"

func handleAdmitMember(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, admitPermission) {
			return
		}

		var body struct {
			PersonID   string  `json:"person_id"`
			PlanID     string  `json:"plan_id"`
			OrgUnitID  *string `json:"org_unit_id"`
			Number     string  `json:"number"`
			ValidFrom  *string `json:"valid_from"`
			ValidUntil *string `json:"valid_until"`
		}
		// The raw bytes, not a re-encoding of the struct: A3.6's digest must
		// be over exactly what the caller sent.
		raw, err := httpx.ReadJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"request body could not be read as JSON")
			return
		}
		if body.PersonID == "" || body.PlanID == "" || body.Number == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"person_id, plan_id and number are required")
			return
		}

		key, err := httpx.IdempotencyKey(r, admitEndpoint, raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		runIdempotent(w, r, pool, logger, key, http.StatusCreated,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				return membership.Admit(ctx, tx, tenantID, membership.NewMembership{
					PersonID:   body.PersonID,
					PlanID:     body.PlanID,
					OrgUnitID:  body.OrgUnitID,
					Number:     body.Number,
					ValidFrom:  body.ValidFrom,
					ValidUntil: body.ValidUntil,
				})
			})
	}
}

// handleListDirectory is the front desk's member list: every membership with
// the holder's name, searchable. Checked as may_approve_membership on the
// tenant — the authority that took the application and so already knows the
// name (A3.12). It is one scope check for the page (ADR-104).
func handleListDirectory(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, admitPermission) {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		tenantID := r.PathValue("tenant")
		var rows []membership.DirectoryEntry
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = membership.ListDirectory(r.Context(), tx, tenantID, r.URL.Query().Get("q"), params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, membership.DirectoryKey))
	}
}
