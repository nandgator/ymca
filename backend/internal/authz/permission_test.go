package authz

import (
	"testing"

	"github.com/nandgator/ymca/backend/internal/auth"
)

// The permission name is the join between three separately-written things:
// A1.2's relations, migration 0002's grantable_permission seed, and
// restriction_kind_permission. 8.1 fixes its form as
// "<object type>.<relation>". If Request disagrees with that spelling, step 2
// silently matches no role_permission rows and every role-derived permission
// stops working with no error anywhere — so this pins it.
func TestRequestPermissionName(t *testing.T) {
	req := Request{
		Object:   Object{Type: "organizational_unit", ID: "u-1", TenantID: "t-1"},
		Relation: "member_read",
	}
	if got, want := req.Permission(), "organizational_unit.member_read"; got != want {
		t.Fatalf("Permission() = %q, want %q", got, want)
	}
}

func TestObjectTypeOf(t *testing.T) {
	t.Run("splits a well-formed name", func(t *testing.T) {
		objectType, relation, err := objectTypeOf("consumption_type.may_close_period")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if objectType != "consumption_type" || relation != "may_close_period" {
			t.Fatalf("got (%q, %q), want (consumption_type, may_close_period)", objectType, relation)
		}
	})

	// A malformed permission name must be an error rather than a lookup that
	// matches nothing. The failure mode this prevents is the quiet one: a
	// name with no dot would query for a permission no row holds, return no
	// assignments, and deny a legitimate role-holder with reason "graph".
	for _, bad := range []string{"member_read", "", ".member_read", "tenant."} {
		t.Run("rejects "+bad, func(t *testing.T) {
			if _, _, err := objectTypeOf(bad); err == nil {
				t.Fatalf("objectTypeOf(%q) returned no error", bad)
			}
		})
	}
}

// Round-trip: whatever Permission() produces, objectTypeOf must be able to
// take apart again into the object type and relation the contextual tuple is
// built from. These two are on opposite sides of step 2 and nothing else
// checks that they agree.
func TestPermissionNameRoundTrips(t *testing.T) {
	req := Request{
		Principal: auth.Principal{ID: "p-1"},
		Object:    Object{Type: "tenant", ID: "t-1", TenantID: "t-1"},
		Relation:  "finance_reader",
	}
	objectType, relation, err := objectTypeOf(req.Permission())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if objectType != req.Object.Type || relation != req.Relation {
		t.Fatalf("round trip gave (%q, %q), want (%q, %q)",
			objectType, relation, req.Object.Type, req.Relation)
	}
}
