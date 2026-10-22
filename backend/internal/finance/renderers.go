package finance

import (
	"encoding/json"
	"fmt"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// The facts a new draft invoice publishes (compose.go). Both are grants, so
// they lag a dispatch interval (8.3); both are written before anybody could
// need them, because nobody can read a draft except through finance_reader.
const (
	EventInvoiceCreated  = "InvoiceCreated"
	EventInvoicePayerSet = "InvoicePayerSet"
)

// Renderers is every event this package publishes. cmd/api registers it with
// the dispatcher; a second package claiming one of these names panics at
// start-up rather than rendering whichever won the map iteration.
func Renderers() map[string]outbox.Renderer {
	return map[string]outbox.Renderer{
		// ADR-018: the tenant edge, without which no check against the
		// invoice can resolve — not even the administrator's.
		EventInvoiceCreated: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				TenantID  string `json:"tenant_id"`
				InvoiceID string `json:"invoice_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			if p.TenantID == "" || p.InvoiceID == "" {
				return nil, fmt.Errorf("renderer: %s payload lacks tenant_id or invoice_id", EventInvoiceCreated)
			}
			return []outbox.Tuple{{
				User:     "tenant:" + p.TenantID,
				Relation: "tenant",
				Object:   "invoice:" + p.InvoiceID,
			}}, nil
		},

		// A1.2: `payer: [person, organizational_unit]` and
		// `viewer: payer or finance_reader from tenant`. The payer is a
		// PERSON, which is why reading an invoice as its payer is checked
		// for the caller's person and not only their principal.
		EventInvoicePayerSet: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				InvoiceID string `json:"invoice_id"`
				PersonID  string `json:"person_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			if p.InvoiceID == "" || p.PersonID == "" {
				return nil, fmt.Errorf("renderer: %s payload lacks invoice_id or person_id", EventInvoicePayerSet)
			}
			return []outbox.Tuple{{
				User:     "person:" + p.PersonID,
				Relation: "payer",
				Object:   "invoice:" + p.InvoiceID,
			}}, nil
		},
	}
}
