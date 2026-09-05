// Package audit writes the one record this step needs from 8.5: every
// DENY, inside the same tenant transaction that produced it (D8).
package audit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// execer is the minimum this package needs to write a row: both pgx.Tx and
// *pgxpool.Pool satisfy it already.
//
// WriteDeny takes a pgx.Tx because D8 requires the DENY and the tenant read
// that produced it to commit together. WritePlatformDeny is looser on
// purpose — see its comment.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// WritePlatformDeny inserts one platform_audit_event row with outcome
// DENIED (ADR-112). It is the platform plane's WriteDeny.
//
// There is no tenantID argument and no db.InTenantTx, because there is no
// tenant: a platform-lifecycle decision names none, which is the whole
// reason this table exists apart from audit_event. A2's claim that
// audit_event.tenant_id was "null for platform-plane" was never true — the
// tenant_isolation policy refuses such a row — and migration 0004 makes
// that column NOT NULL rather than leaving the false promise standing.
//
// It takes an execer rather than a pgx.Tx because, unlike a tenant-plane
// DENY, there is frequently no business transaction for this row to be
// atomic with: a refusal at the edge has performed no work to be atomic
// against. Callers that DO have a transaction should pass it.
//
// One consequence worth knowing before you write anything here: the
// application holds INSERT on this table and not SELECT (ADR-112), so it
// cannot read back what it writes. Nothing may be recorded here that the
// code later needs to query — this is an evidence trail for a human or an
// operator role, never application state.
func WritePlatformDeny(ctx context.Context, db execer, ev DenyEvent) error {
	_, err := db.Exec(ctx, `
		INSERT INTO platform_audit_event
		    (actor_principal_id, action, object_type, object_id,
		     outcome, severity, context)
		VALUES
		    (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, '')::uuid,
		     'DENIED', $5, jsonb_build_object('request_id', $6::text, 'reason', $7::text))
	`, ev.ActorPrincipalID, ev.Action, ev.ObjectType, ev.ObjectID,
		ev.Severity, ev.RequestID, ev.Reason)
	if err != nil {
		return fmt.Errorf("audit: write platform deny event: %w", err)
	}
	return nil
}
