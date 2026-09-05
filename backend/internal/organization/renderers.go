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
	}
}
