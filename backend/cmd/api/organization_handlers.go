// organization_handlers.go is A3.7's people-and-units surface: registering a
// person, creating a unit with its authorization edge, reading a unit, and
// listing the memberships scoped to one.
//
// Three of the four check a permission that no other endpoint checks, and
// each was chosen against a narrower alternative:
//
//	POST /people          tenant.may_register_person   ADR-114, not admin:
//	                      a front desk takes applications (05.3.4)
//	POST /units           admin on the NAMED PARENT     tenant for a
//	                      top-level unit, the parent unit for a nested one
//	GET  /units/{unit}    member on the unit            a tenant admin
//	                      reaches it via `member from auth_parent`
//	GET  .../members      member_read on the unit       ADR-104: the list
//	                      authorizes the scope, never the row
package main

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/identity"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/organization"
	"github.com/nandgator/ymca/backend/internal/page"
)

// handleRegisterPerson creates a global person row under a tenant permission
// (ADR-114).
//
// The endpoint is tenant-scoped and the row is not, which is the whole of
// ADR-114: `person` carries no tenant_id and can state no opinion about who
// may create it, so the permission sits on the tenant the caller is acting
// in. Nothing about the created row belongs to that tenant afterwards.
func handleRegisterPerson(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, "may_register_person") {
			return
		}
		var body struct {
			GivenName         *string `json:"given_name"`
			FamilyName        *string `json:"family_name"`
			DisplayName       string  `json:"display_name"`
			DateOfBirth       *string `json:"date_of_birth"`
			PreferredLanguage *string `json:"preferred_language"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.DisplayName == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "display_name is required")
			return
		}

		tenantID := r.PathValue("tenant")
		var person identity.Person
		// InTenantTx, though person is one of A2.1's RLS exemptions and needs
		// no tenant context. It runs here anyway so that admission — which
		// will register and admit in one transaction — does not have to
		// change which transaction helper this call sits in later.
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			person, err = identity.RegisterPerson(r.Context(), tx, identity.NewPerson{
				GivenName:         body.GivenName,
				FamilyName:        body.FamilyName,
				DisplayName:       body.DisplayName,
				DateOfBirth:       body.DateOfBirth,
				PreferredLanguage: body.PreferredLanguage,
			})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, person)
	}
}

// handleCreateUnit creates a unit and its first authorization edge.
//
// The check is `admin` on the parent the REQUEST names, not on the tenant.
// That is the difference between "may administer this association" and "may
// add a unit beneath this one", and it is what lets a branch's own
// administrator create a sub-unit without holding authority over the whole
// association. A tenant admin still passes it for a top-level unit, because
// the parent they name is the tenant.
//
// Note the consequence of ADR-101's asymmetry for a caller creating two
// levels at once: the new unit's `auth_parent` tuple is a GRANT and reaches
// OpenFGA only when the dispatcher runs, so an immediate request to create a
// child beneath it is refused until then. Grants may lag; that is the design,
// not a race here.
func handleCreateUnit(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Type        string  `json:"type"`
			Name        string  `json:"name"`
			OrgParentID *string `json:"org_parent_id"`
			Parent      struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"parent"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.Name == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "name is required")
			return
		}
		if body.Parent.Type == "" || body.Parent.ID == "" {
			// A unit with no authorization parent is administrable by
			// nobody and would return 201 all the same — ADR-113's silent
			// shape, one object down.
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"parent is required: a unit with no authorization parent is unreachable")
			return
		}

		// The body is read BEFORE the check, unlike every other handler
		// here, because the check's object comes out of the body. The order
		// discloses nothing: a malformed body is refused without consulting
		// the graph, and a well-formed one is authorized before a row is
		// written.
		tenantID := r.PathValue("tenant")
		switch body.Parent.Type {
		case "tenant":
			// Object.TenantID is the PARENT's own id, so naming another
			// tenant trips 6.1 step 1 and is audited as a tenant mismatch
			// rather than as an ordinary DENY. That row is the one that
			// evidences somebody probing for tenants they cannot reach.
			if !authorizeObject(w, r, pool, fga, logger, authz.Object{
				Type: "tenant", ID: body.Parent.ID, TenantID: body.Parent.ID,
			}, "admin") {
				return
			}
		case "organizational_unit":
			if !authorizeUnit(w, r, pool, fga, logger, body.Parent.ID, "admin") {
				return
			}
		default:
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"parent.type must be tenant or organizational_unit")
			return
		}

		principal, _ := httpx.PrincipalFromContext(r.Context())
		var unit organization.Unit
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			unit, err = organization.CreateUnit(r.Context(), tx, tenantID, organization.NewUnit{
				Type:        body.Type,
				Name:        body.Name,
				OrgParentID: body.OrgParentID,
				Parent: organization.Parent{
					Type: body.Parent.Type,
					ID:   body.Parent.ID,
				},
				CreatedBy: principal.ID,
			})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, unit)
	}
}

// handleGetUnit reads one unit.
//
// `member`, not `admin`: 05.1 makes a unit's existence and shape ordinary
// information to anyone who belongs to it. A tenant admin reaches it through
// `member from auth_parent` (A1.2), which is why no relation had to be added
// for this endpoint.
func handleGetUnit(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unitID := r.PathValue("unit")
		if !authorizeUnit(w, r, pool, fga, logger, unitID, "member") {
			return
		}

		tenantID := r.PathValue("tenant")
		var unit organization.Unit
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			unit, err = organization.GetUnit(r.Context(), tx, tenantID, unitID)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, unit)
	}
}

// handleListUnitMembers is ADR-104 in one function: one check against the
// SCOPE, then keyset SQL under RLS. There is no per-row check, and 8.11
// forbids the ListObjects call that would replace it.
//
// The rows are the named unit's own memberships and no descendant's. See
// membership.ListUnitMembers for why that under-report is the correct
// direction to err in.
func handleListUnitMembers(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unitID := r.PathValue("unit")
		if !authorizeUnit(w, r, pool, fga, logger, unitID, "member_read") {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		var rows []membership.UnitMember
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = membership.ListUnitMembers(r.Context(), tx, tenantID, unitID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, membership.UnitMemberKey))
	}
}
