// renderers.go turns the facts this package publishes into the tuples the
// CURRENT model wants (ADR-101). Keeping them beside the producers means a
// change to a fact and a change to its rendering are one diff, and the
// dispatcher's "no renderer" failure names the event that is missing one.
package membership

import (
	"encoding/json"
	"fmt"

	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// Renderers is every event this package and internal/consumption publish.
// cmd/api registers it with the dispatcher.
func Renderers() map[string]outbox.Renderer {
	return map[string]outbox.Renderer{
		EventBundleCreated:           tenantEdge("bundle_id", "entitlement_bundle"),
		EventPlanCreated:             tenantEdge("plan_id", "membership_plan"),
		consumption.EventTypeCreated: tenantEdge("type_id", "consumption_type"),

		EventBundleEntitles: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				BundleID   string `json:"bundle_id"`
				ObjectType string `json:"object_type"`
				ObjectID   string `json:"object_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			// The userset, not the bundle. What reaches the object is every
			// person the bundle benefits.
			return []outbox.Tuple{{
				User:     "entitlement_bundle:" + p.BundleID + "#beneficiary",
				Relation: "entitled",
				Object:   p.ObjectType + ":" + p.ObjectID,
			}}, nil
		},

		EventPlanGrants: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				PlanID   string `json:"plan_id"`
				BundleID string `json:"bundle_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			// ADR-107's direction: the BUNDLE names the plan conferring it.
			// The reverse reads naturally and resolves to nothing.
			return []outbox.Tuple{{
				User:     "membership_plan:" + p.PlanID,
				Relation: "via_plan",
				Object:   "entitlement_bundle:" + p.BundleID,
			}}, nil
		},
	}
}

// tenantEdge renders `tenant:<t> tenant <type>:<id>`, the edge ADR-018
// requires before any check against the object can resolve.
func tenantEdge(idField, objectType string) outbox.Renderer {
	return func(payload json.RawMessage) ([]outbox.Tuple, error) {
		var p map[string]string
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		tenantID, id := p["tenant_id"], p[idField]
		if tenantID == "" || id == "" {
			return nil, fmt.Errorf("renderer: payload lacks tenant_id or %s", idField)
		}
		return []outbox.Tuple{{
			User:     "tenant:" + tenantID,
			Relation: "tenant",
			Object:   objectType + ":" + id,
		}}, nil
	}
}
