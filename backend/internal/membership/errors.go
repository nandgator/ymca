package membership

import "errors"

// ErrNotFound is A3.3's 404, which is deliberately indistinguishable from a
// denial: "absent, OR the caller may not know that it does exist".
var ErrNotFound = errors.New("membership: not found")

// ErrInvalidEntitledType is A3.4's invalid_request: the caller named an
// object type that cannot carry `entitled`. It is the caller's own input, so
// the message may name it (A3.4).
var ErrInvalidEntitledType = errors.New("membership: object type cannot be entitled")
