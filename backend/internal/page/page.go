// Package page implements A3.5 and 8.11's keyset pagination.
//
// Keyset, not offset, and the reason is specific to this system: offset
// pagination over a table being written to skips and repeats rows, and
// consumption records are written continuously during service hours. A warden
// paging through today's meals with an offset would see some twice and miss
// others, with nothing to indicate it.
//
// The cursor is opaque so the SORT can change without breaking pagers
// (8.11) — that is a decoupling guarantee, not a security one. It is not
// signed and does not need to be: a forged cursor selects a different
// starting key within rows the caller could already page to, because RLS and
// the list's own scope check (ADR-104) decide what is visible, not the
// cursor. If a cursor ever encodes something that is not a sort key, that
// reasoning stops holding.
//
// Transport-neutral by intent (REVIEW.md B7): a CLI listing consumption
// records pages exactly the same way, so nothing here mentions HTTP.
package page

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// DefaultLimit and MaxLimit are A3.5's "limit has a server maximum".
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// ErrInvalidCursor is A3.4's invalid_request for a cursor that does not
// decode. It is deliberately not treated as "no cursor": silently restarting
// from the beginning returns the wrong page and says nothing about it.
var ErrInvalidCursor = errors.New("page: cursor is not one this server issued")

// ErrInvalidLimit is A3.4's invalid_request for a limit outside its range.
var ErrInvalidLimit = errors.New("page: limit out of range")

// Params is a decoded page request. Key is the sort key of the last row of
// the previous page, or nil for the first page.
type Params struct {
	Limit int
	Key   []string
}

// cursorBody is what a cursor encodes. The version field exists so that a
// later change to the encoding can reject old cursors deliberately rather
// than misreading them.
type cursorBody struct {
	V   int      `json:"v"`
	Key []string `json:"k"`
}

const cursorVersion = 1

// Encode renders a sort key as an opaque cursor.
func Encode(key []string) string {
	if len(key) == 0 {
		return ""
	}
	raw, err := json.Marshal(cursorBody{V: cursorVersion, Key: key})
	if err != nil {
		// cursorBody is a struct of strings and an int; Marshal cannot fail
		// on it. Returning "" would silently reset a pager, so this panics
		// rather than lying about where the page ends.
		panic("page: encoding a cursor: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode reads a cursor this server issued.
func Decode(cursor string) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var body cursorBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, ErrInvalidCursor
	}
	if body.V != cursorVersion || len(body.Key) == 0 {
		return nil, ErrInvalidCursor
	}
	return body.Key, nil
}

// Parse turns a raw limit and cursor into Params. An empty limit takes
// DefaultLimit; anything above MaxLimit is refused rather than clamped, so a
// client asking for 10,000 rows learns that it did not get them.
func Parse(limit int, hasLimit bool, cursor string) (Params, error) {
	p := Params{Limit: DefaultLimit}
	if hasLimit {
		if limit < 1 || limit > MaxLimit {
			return Params{}, fmt.Errorf("%w: %d is not between 1 and %d",
				ErrInvalidLimit, limit, MaxLimit)
		}
		p.Limit = limit
	}

	key, err := Decode(cursor)
	if err != nil {
		return Params{}, err
	}
	p.Key = key
	return p, nil
}

// Fetch is how many rows to ask the database for: one more than the page, so
// that the presence of a next page is known without a second COUNT query over
// a table that is being written to.
func (p Params) Fetch() int { return p.Limit + 1 }

// Page is A3.5's response shape.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// Of trims a Fetch()-sized result to one page and builds the next cursor from
// the last row that survives the trim.
//
// keyOf must return the same sort key the query ordered by. A mismatch is the
// one way to break a keyset pager subtly rather than loudly: pages would
// overlap or skip, and nothing would report it.
func Of[T any](rows []T, p Params, keyOf func(T) []string) Page[T] {
	if len(rows) <= p.Limit {
		// Items is initialised so that an empty page marshals as [] rather
		// than null. A client distinguishing those is a bug waiting to be
		// written on both sides.
		items := rows
		if items == nil {
			items = []T{}
		}
		return Page[T]{Items: items}
	}
	items := rows[:p.Limit]
	return Page[T]{Items: items, NextCursor: Encode(keyOf(items[len(items)-1]))}
}
