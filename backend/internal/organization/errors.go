// errors.go is this package's domain vocabulary. cmd/api maps each of these
// to one of A3.4's codes; nothing here knows what an HTTP status is (B7).
package organization

import "errors"

// ErrNotFound is A3.3's 404, deliberately indistinguishable from a denial:
// "absent, OR the caller may not know that it does exist".
var ErrNotFound = errors.New("organization: not found")

// ErrOwnerSubjectTaken is A3.4's invalid_request: the IdP subject offered
// for the owner already identifies a principal. It names a value the caller
// supplied, so echoing it discloses nothing they did not already send.
var ErrOwnerSubjectTaken = errors.New("organization: owner idp_subject already identifies a principal")

// ErrInvalidUnitType is A3.4's invalid_request for a unit type outside
// 05.1.2's six. The value is the caller's own, so the message may name it.
var ErrInvalidUnitType = errors.New("organization: not an organizational unit type")

// ErrInvalidParentType is A3.4's invalid_request for an authorization parent
// that is neither a tenant nor a unit. A1.2's `auth_parent` type restriction
// on organizational_unit is [organizational_unit, tenant] and nothing else.
var ErrInvalidParentType = errors.New("organization: not an authorization parent type")

// The three refusals migration 0005's trigger can raise (ADR-115). They are
// errors of this package rather than of the database because the trigger is
// where the invariant LIVES, not where it belongs conceptually: ADR-016
// stated all three years before there was a table to put them on.
var (
	// ErrEdgeCycle is ADR-016 invariant 1. Unreachable from
	// POST /t/{t}/units — a unit nothing points at yet cannot close a cycle
	// — which is precisely why the check is not in that endpoint.
	ErrEdgeCycle = errors.New("organization: the edge would close a cycle in the authorization DAG")

	// ErrEdgeTooDeep is invariant 3, and the one this slice can actually
	// provoke: twelve nested units are legal and the thirteenth is not.
	ErrEdgeTooDeep = errors.New("organization: the edge would exceed the authorization path depth limit")

	// ErrEdgeForeignEndpoint is invariant 2. It reaches a caller only as a
	// parent that does not resolve in this tenant, which A3.3 makes
	// indistinguishable from one that does not exist.
	ErrEdgeForeignEndpoint = errors.New("organization: an edge endpoint does not resolve to this tenant")
)

// uniqueViolation is the SQLSTATE for a unique or primary key collision.
const uniqueViolation = "23505"

// checkViolation is the SQLSTATE migration 0005's trigger raises. Which
// invariant failed is carried in the constraint name, not in the message —
// the message is for a log, and A3.4 forbids putting it in a response.
const checkViolation = "23514"
