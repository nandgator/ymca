// request.go reads what A3 puts on a request: a JSON body, the pagination
// parameters of A3.5, and the idempotency key of A3.6. Each is read once,
// here, so that eighteen handlers cannot disagree about what a malformed
// limit means.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/nandgator/ymca/backend/internal/idempotency"
	"github.com/nandgator/ymca/backend/internal/page"
)

// MaxBodyBytes bounds a request body. A3 sets no figure; this is small
// enough that no legitimate request in the slice approaches it and large
// enough that none is refused. C5 records that there is no rate limiting or
// abuse model, of which this is a small part rather than a substitute.
const MaxBodyBytes = 1 << 20 // 1 MiB

// ErrBodyTooLarge is separated from a decode failure so the message can say
// which happened without echoing the body back.
var ErrBodyTooLarge = errors.New("httpx: request body too large")

// ReadJSON decodes a request body into v and returns the raw bytes, which
// A3.6 needs for the idempotency digest — the digest must be over exactly
// what the caller sent, not over a re-encoding of the decoded struct, or two
// bodies differing only in field order would hash the same.
//
// Unknown fields are rejected. A caller who misspells a field would
// otherwise have it silently ignored and get a successful response for a
// request the server did not perform.
func ReadJSON(r *http.Request, v any) ([]byte, error) {
	limited := http.MaxBytesReader(nil, r.Body, MaxBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, ErrBodyTooLarge
		}
		return nil, fmt.Errorf("httpx: read body: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return nil, fmt.Errorf("httpx: decode body: %w", err)
	}
	return raw, nil
}

// PageParams reads A3.5's limit and cursor from the query string.
func PageParams(r *http.Request) (page.Params, error) {
	q := r.URL.Query()

	limit, hasLimit := 0, false
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return page.Params{}, fmt.Errorf("%w: limit %q is not a number",
				page.ErrInvalidLimit, raw)
		}
		limit, hasLimit = n, true
	}
	return page.Parse(limit, hasLimit, q.Get("cursor"))
}

// IdempotencyHeader is A3.6's carrier.
const IdempotencyHeader = "Idempotency-Key"

// ErrMissingIdempotencyKey is returned for a POST that A3.6 requires a key
// on. It is a 400 rather than a silent pass: a retried meal without a key is
// a second meal, and accepting the request would hide that from the caller.
var ErrMissingIdempotencyKey = errors.New("httpx: " + IdempotencyHeader + " is required on this request")

// IdempotencyKey builds the key for a request. endpoint is the route
// pattern, not the resolved path: two records against different consumption
// types are different operations, and their ids are already inside the body
// digest.
func IdempotencyKey(r *http.Request, endpoint string, body []byte) (idempotency.Key, error) {
	raw := r.Header.Get(IdempotencyHeader)
	if raw == "" {
		return idempotency.Key{}, ErrMissingIdempotencyKey
	}
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return idempotency.Key{}, errors.New("httpx: idempotency key built before authentication")
	}
	return idempotency.Key{
		TenantID:      r.PathValue("tenant"),
		Endpoint:      endpoint,
		Key:           raw,
		PrincipalID:   principal.ID,
		RequestDigest: idempotency.Digest(body),
	}, nil
}
