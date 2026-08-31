// Package audit writes the one record this step needs from 8.5: every
// DENY, inside the same tenant transaction that produced it (D8).
package audit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DenyEvent is the input to WriteDeny — the fields of 8.5's record shape
// this step actually produces. tenant_id, outcome and occurred_at are
// supplied by WriteDeny itself, not by the caller.
type DenyEvent struct {
	// ActorPrincipalID identifies who was denied. D8 carves out one case
	// with no principal at all — an authentication failure — and routes it
	// to the structured log instead of here; a caller with no principal
	// must not call WriteDeny.
	ActorPrincipalID string
	Action           string
	ObjectType       string
	ObjectID         string
	// Severity is 8.5's INFO | NOTABLE | HIGH.
	Severity string
	// Reason names which check produced the DENY (e.g. "graph",
	// "principal_not_active"). A2 gives audit_event no column for this, so
	// it is folded into context alongside the request id.
	Reason    string
	RequestID string
}

// WriteDeny inserts one audit_event row with outcome DENIED, using tx.
//
// The two jsonb_build_object arguments are cast explicitly: the function
// takes "any", so PostgreSQL cannot infer a parameter's type from the call
// site and rejects the statement outright (42P18) without the ::text.
//
// tx must already be running inside a transaction that has named tenantID
// (db.InTenantTx) — audit_event carries the same tenant_isolation policy as
// every other tenant-scoped table (0001_init.sql), so an INSERT attempted
// outside that context, or naming the wrong tenant, is rejected by
// PostgreSQL itself rather than by this function.
func WriteDeny(ctx context.Context, tx pgx.Tx, tenantID string, ev DenyEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_event
		    (tenant_id, actor_principal_id, action, object_type, object_id,
		     outcome, severity, context)
		VALUES
		    ($1, NULLIF($2, '')::uuid, $3, $4, NULLIF($5, '')::uuid,
		     'DENIED', $6, jsonb_build_object('request_id', $7::text, 'reason', $8::text))
	`, tenantID, ev.ActorPrincipalID, ev.Action, ev.ObjectType, ev.ObjectID,
		ev.Severity, ev.RequestID, ev.Reason)
	if err != nil {
		return fmt.Errorf("audit: write deny event: %w", err)
	}
	return nil
}
