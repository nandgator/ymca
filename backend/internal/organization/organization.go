// Package organization owns tenants and organizational units — the shape of
// an association, and the DAG authority travels down (05.1, A2.2).
//
// Transport-neutral by intent (REVIEW.md B7): every function takes a
// transaction and returns a value, so an operator CLI and an HTTP handler
// call the same code.
//
// Provisioning is the one thing here that does NOT run inside
// db.InTenantTx. tenant, person, principal and authorization_outbox all
// carry no tenant_id and none is under row-level security, and there is no
// tenant context to run inside because the tenant is what is being created.
// db.InTx exists for exactly this.
package organization

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// EventTenantProvisioned is published when a tenant and its first owner are
// created together. Its renderer is in renderers.go.
const EventTenantProvisioned = "TenantProvisioned"

// ErrOwnerSubjectTaken is A3.4's invalid_request: the IdP subject offered
// for the owner already identifies a principal. It names a value the caller
// supplied, so echoing it discloses nothing they did not already send.
var ErrOwnerSubjectTaken = errors.New("organization: owner idp_subject already identifies a principal")

// uniqueViolation is the SQLSTATE for a unique or primary key collision.
const uniqueViolation = "23505"

// Owner is the first principal a tenant ever has.
type Owner struct {
	PersonID    string `json:"person_id"`
	PrincipalID string `json:"principal_id"`
}

// Tenant is a provisioned tenant and the owner provisioned with it.
type Tenant struct {
	ID           string `json:"id"`
	LegalName    string `json:"legal_name"`
	DisplayName  string `json:"display_name"`
	Jurisdiction string `json:"jurisdiction"`
	Status       string `json:"status"`
	Owner        Owner  `json:"owner"`
}

// NewTenant is what a caller must supply to provision one.
type NewTenant struct {
	LegalName        string
	DisplayName      string
	Jurisdiction     string
	OwnerDisplayName string
	OwnerIDPSubject  string
}

// CreateTenant provisions a tenant AND its first owner, in one transaction
// (ADR-113).
//
// The owner is not a convenience. Every write past this point authorizes
// through `admin` on the tenant, or through something that resolves to it,
// and `tenant.admin` is `[principal] or owner`. A tenant created without an
// owner principal is a tenant no one can ever create a unit, a plan or a
// person in — and nothing would say so. The endpoint would return 201, the
// row would exist, and the association would be unreachable.
//
// That silent shape is the same failure ADR-107 describes for a member with
// no entitlements: the operation succeeds and the thing it made does
// nothing. It is why the owner tuple is enqueued in the same transaction as
// the rows it describes rather than left to a later call.
//
// The principal's kind is STAFF (05.2.3): the owner acts for the
// association from the moment it exists, not as themselves. A PERSONAL
// principal here would make the tenant's own administration
// indistinguishable from that human's private use of it.
func CreateTenant(ctx context.Context, tx pgx.Tx, in NewTenant) (Tenant, error) {
	t := Tenant{
		LegalName:    in.LegalName,
		DisplayName:  in.DisplayName,
		Jurisdiction: in.Jurisdiction,
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES (gen_random_uuid(), $1, $2, $3, 'ACTIVE')
		RETURNING id::text, status
	`, in.LegalName, in.DisplayName, in.Jurisdiction).Scan(&t.ID, &t.Status)
	if err != nil {
		return Tenant{}, fmt.Errorf("organization: create tenant: %w", err)
	}

	// person is global — no tenant_id, exempt from RLS by design (A2.1,
	// 05.2.2). The owner is a person in the movement who happens to be this
	// association's first administrator, not a row belonging to it.
	err = tx.QueryRow(ctx, `
		INSERT INTO person (id, display_name, status)
		VALUES (gen_random_uuid(), $1, 'ACTIVE')
		RETURNING id::text
	`, in.OwnerDisplayName).Scan(&t.Owner.PersonID)
	if err != nil {
		return Tenant{}, fmt.Errorf("organization: create owner person: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO principal (id, person_id, idp_subject, kind, label, status)
		VALUES (gen_random_uuid(), $1::uuid, $2, 'STAFF', $3, 'ACTIVE')
		RETURNING id::text
	`, t.Owner.PersonID, in.OwnerIDPSubject, "owner of "+in.DisplayName).
		Scan(&t.Owner.PrincipalID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			// principal.idp_subject is UNIQUE (A2.3): one IdP subject is one
			// principal, globally. Offering a taken one is a caller error,
			// not a server fault.
			return Tenant{}, ErrOwnerSubjectTaken
		}
		return Tenant{}, fmt.Errorf("organization: create owner principal: %w", err)
	}

	// The grant may lag by a dispatch interval and that is acceptable
	// (ADR-101): nobody is waiting to administer a tenant that did not exist
	// a moment ago. It is fenced all the same, so a later revocation of this
	// ownership cannot be overtaken by this row.
	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "tenant",
		AggregateID:   t.ID,
		EventType:     EventTenantProvisioned,
		Payload: map[string]string{
			"tenant_id":    t.ID,
			"principal_id": t.Owner.PrincipalID,
			"person_id":    t.Owner.PersonID,
		},
		Fence: outbox.Fence{
			Subject:  "principal:" + t.Owner.PrincipalID,
			Relation: "owner",
			Object:   "tenant:" + t.ID,
		},
	}); err != nil {
		return Tenant{}, err
	}

	return t, nil
}
