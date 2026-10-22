// Package consumption owns consumption types and, later, the records made
// against them (05.10, A2.9).
//
// Transport-neutral (REVIEW.md B7): a warden's phone and an operator CLI
// call the same functions.
package consumption

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/outbox"
	"github.com/nandgator/ymca/backend/internal/page"
)

// EventTypeCreated is published when a consumption type is defined.
const EventTypeCreated = "ConsumptionTypeCreated"

// ErrNotFound is A3.3's 404.
var ErrNotFound = errors.New("consumption: not found")

// Type is a consumption type (A2.9).
type Type struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Obligates  bool    `json:"obligates"`
	Recurrence *string `json:"recurrence"`
	RecordMode string  `json:"record_mode"`
	Status     string  `json:"status"`
	// CorrectionWindowDays bounds how late a correction may be filed
	// (05.10.7); nil means no limit.
	CorrectionWindowDays *int `json:"correction_window_days"`
}

// CreateType defines a consumption type and queues its tenant edge.
//
// A2.9's CHECK requires a recurrence whenever the type obligates, which is
// ADR-097's opt-in machinery: dinner obligates and breakfast does not, so
// the per-meal absence default of the prototype disappears rather than being
// configured. The database enforces it; this is where the caller's error
// becomes a 400 instead of a constraint violation.
func CreateType(ctx context.Context, tx pgx.Tx, tenantID string, t Type) (Type, error) {
	if t.CorrectionWindowDays != nil && *t.CorrectionWindowDays < 0 {
		return Type{}, fmt.Errorf("consumption: correction_window_days cannot be negative")
	}
	if t.Obligates && (t.Recurrence == nil || *t.Recurrence == "") {
		return Type{}, fmt.Errorf(
			"consumption: a type that obligates must declare a recurrence (A2.9, ADR-097)")
	}

	var out Type
	err := tx.QueryRow(ctx, `
		INSERT INTO consumption_type
		    (id, tenant_id, name, obligates, recurrence, record_mode, status,
		     correction_window)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, 'ACTIVE',
		        CASE WHEN $6::int IS NULL THEN NULL ELSE make_interval(days => $6::int) END)
		RETURNING id::text, name, obligates, recurrence, record_mode, status,
		          EXTRACT(day FROM correction_window)::int
	`, tenantID, t.Name, t.Obligates, t.Recurrence, t.RecordMode, t.CorrectionWindowDays).
		Scan(&out.ID, &out.Name, &out.Obligates, &out.Recurrence, &out.RecordMode,
			&out.Status, &out.CorrectionWindowDays)
	if err != nil {
		return Type{}, fmt.Errorf("consumption: create type: %w", err)
	}

	if err := outbox.Enqueue(ctx, tx, outbox.Fact{
		AggregateType: "consumption_type",
		AggregateID:   out.ID,
		EventType:     EventTypeCreated,
		Payload:       map[string]string{"tenant_id": tenantID, "type_id": out.ID},
		Fence: outbox.Fence{
			Subject:  "tenant:" + tenantID,
			Relation: "tenant",
			Object:   "consumption_type:" + out.ID,
		},
	}); err != nil {
		return Type{}, err
	}
	return out, nil
}

// ListTypes pages consumption types under the tenant scope (ADR-104).
func ListTypes(ctx context.Context, tx pgx.Tx, tenantID string, p page.Params) ([]Type, error) {
	var afterName, afterID string
	if len(p.Key) == 2 {
		afterName, afterID = p.Key[0], p.Key[1]
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, name, obligates, recurrence, record_mode, status,
		       EXTRACT(day FROM correction_window)::int
		  FROM consumption_type
		 WHERE tenant_id = $1
		   AND ($2 = '' OR (name, id::text) > ($2, $3))
		 ORDER BY name, id
		 LIMIT $4
	`, tenantID, afterName, afterID, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("consumption: list types: %w", err)
	}
	defer rows.Close()

	var out []Type
	for rows.Next() {
		var t Type
		if err := rows.Scan(&t.ID, &t.Name, &t.Obligates, &t.Recurrence,
			&t.RecordMode, &t.Status, &t.CorrectionWindowDays); err != nil {
			return nil, fmt.Errorf("consumption: scan type: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TypeKey is the sort key ListTypes orders by.
func TypeKey(t Type) []string { return []string{t.Name, t.ID} }

// ListActive pages the tenant's ACTIVE types by name. The handler filters the
// page to those the caller may record against (GET /me/consumption-types), so
// a returned page can be shorter than its limit without being the last one.
func ListActive(ctx context.Context, tx pgx.Tx, tenantID string, p page.Params) ([]Type, error) {
	var afterName, afterID string
	if len(p.Key) == 2 {
		afterName, afterID = p.Key[0], p.Key[1]
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, name, obligates, recurrence, record_mode, status,
		       EXTRACT(day FROM correction_window)::int
		  FROM consumption_type
		 WHERE tenant_id = $1 AND status = 'ACTIVE'
		   AND ($2 = '' OR (name, id::text) > ($2, $3))
		 ORDER BY name, id
		 LIMIT $4
	`, tenantID, afterName, afterID, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("consumption: list active types: %w", err)
	}
	defer rows.Close()

	var out []Type
	for rows.Next() {
		var t Type
		if err := rows.Scan(&t.ID, &t.Name, &t.Obligates, &t.Recurrence,
			&t.RecordMode, &t.Status, &t.CorrectionWindowDays); err != nil {
			return nil, fmt.Errorf("consumption: scan type: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
