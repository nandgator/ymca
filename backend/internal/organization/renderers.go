// renderers.go turns this package's facts into the tuples the CURRENT model
// wants (ADR-101), living beside the code that publishes them so that a
// change to a fact and a change to its rendering are one diff.
package organization

import (
	"encoding/json"
	"fmt"

	"github.com/nandgator/ymca/backend/internal/outbox"
)

// Renderers is every event this package publishes. cmd/api merges it with
// the other packages' renderers and registers the result with the
// dispatcher; an event arriving with no renderer fails its row loudly
// rather than being skipped.
func Renderers() map[string]outbox.Renderer {
	return map[string]outbox.Renderer{
		EventTenantProvisioned: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				TenantID    string `json:"tenant_id"`
				PrincipalID string `json:"principal_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			if p.TenantID == "" || p.PrincipalID == "" {
				return nil, fmt.Errorf("renderer: %s payload lacks tenant_id or principal_id",
					EventTenantProvisioned)
			}
			// `owner`, not `admin`. A1.2 defines tenant.admin as
			// "[principal] or owner", so owner reaches admin and is the
			// stronger, more truthful statement about who this is: the
			// association's first authority, from whom every later admin
			// derives.
			return []outbox.Tuple{{
				User:     "principal:" + p.PrincipalID,
				Relation: "owner",
				Object:   "tenant:" + p.TenantID,
			}}, nil
		},

		// The unit's tenant edge. ADR-018: no permission resolves without a
		// tenant in the path, so this is what makes the unit reachable at
		// all — `organizational_unit.admin` includes
		// `administered_by from tenant`, which has nothing to traverse
		// without it.
		EventUnitCreated: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				TenantID string `json:"tenant_id"`
				UnitID   string `json:"unit_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			if p.TenantID == "" || p.UnitID == "" {
				return nil, fmt.Errorf("renderer: %s payload lacks tenant_id or unit_id",
					EventUnitCreated)
			}
			return []outbox.Tuple{{
				User:     "tenant:" + p.TenantID,
				Relation: "tenant",
				Object:   "organizational_unit:" + p.UnitID,
			}}, nil
		},

		// The DAG edge itself. The direction is the one that catches people:
		// the PARENT is the user and the CHILD is the object, because
		// A1.2 declares `auth_parent` ON organizational_unit and OpenFGA
		// traverses forward from the object. Written the other way round it
		// reads just as naturally and resolves to nothing — the ADR-107
		// mistake, in a second place.
		EventUnitAuthParent: func(payload json.RawMessage) ([]outbox.Tuple, error) {
			var p struct {
				UnitID     string `json:"unit_id"`
				ParentType string `json:"parent_type"`
				ParentID   string `json:"parent_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return nil, err
			}
			if p.UnitID == "" || p.ParentType == "" || p.ParentID == "" {
				return nil, fmt.Errorf("renderer: %s payload lacks unit_id or a parent",
					EventUnitAuthParent)
			}
			return []outbox.Tuple{{
				User:     Parent{Type: p.ParentType, ID: p.ParentID}.Object(),
				Relation: "auth_parent",
				Object:   "organizational_unit:" + p.UnitID,
			}}, nil
		},
	}
}
