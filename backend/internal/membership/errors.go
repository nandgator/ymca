package membership

import "errors"

// ErrNotFound is A3.3's 404, which is deliberately indistinguishable from a
// denial: "absent, OR the caller may not know that it does exist".
var ErrNotFound = errors.New("membership: not found")

// ErrInvalidEntitledType is A3.4's invalid_request: the caller named an
// object type that cannot carry `entitled`. It is the caller's own input, so
// the message may name it (A3.4).
var ErrInvalidEntitledType = errors.New("membership: object type cannot be entitled")

// The two ways admission collides, and they are separate codes on the wire
// rather than one `conflict` with two messages. A3.4 writes the message for
// a developer, so a client must not parse it — if the two cases call for
// different client behaviour, and they do, only the code can carry that.
var (
	// ErrNumberTaken is A3.7's 409 on `UNIQUE (tenant_id, number)`. The
	// number is the caller's own (deliberately not ADR-103's counter), so
	// the answer is "choose another".
	ErrNumberTaken = errors.New("membership: that membership number is already in use in this tenant")

	// ErrAlreadyActiveMember is the partial unique index
	// one_active_membership_per_tenant: one person holds at most one live
	// membership per tenant (ADR-043, 05.3.5). Five programme
	// subscriptions do not make five memberships, so the answer is not
	// "choose another number" but "this person is already a member".
	ErrAlreadyActiveMember = errors.New("membership: that person already holds an active membership in this tenant")
)

// ErrPlanNotOpen is A3.4's invalid_request: the plan exists in this tenant
// and does not accept applicants. 05.3.2 makes CLOSED_TO_NEW the common real
// state — existing life members keep a plan that closed years ago — and
// DRAFT is a plan not yet live. The status is the tenant's own configuration
// and the caller named the plan, so the message may say which.
var ErrPlanNotOpen = errors.New("membership: that plan does not accept new members")

// The SQLSTATEs admission maps. Which constraint produced one is carried in
// the constraint name, not in the message: the message is written for a log
// and A3.4 forbids putting it in a response.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)
