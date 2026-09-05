package page

import (
	"encoding/json"
	"errors"
	"testing"
)

type row struct {
	CreatedAt string
	ID        string
}

func keyOf(r row) []string { return []string{r.CreatedAt, r.ID} }

func rows(n int) []row {
	out := make([]row, n)
	for i := range out {
		out[i] = row{CreatedAt: "2026-09-05T00:00:0" + string(rune('0'+i%10)) + "Z", ID: string(rune('a' + i))}
	}
	return out
}

func TestCursorRoundTrip(t *testing.T) {
	key := []string{"2026-09-05T10:00:00Z", "0191d2f0-0000-7000-8000-000000000001"}
	got, err := Decode(Encode(key))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(got) != len(key) || got[0] != key[0] || got[1] != key[1] {
		t.Fatalf("round trip gave %v, want %v", got, key)
	}
}

// A cursor must be opaque in the sense 8.11 means: a client cannot read the
// sort key out of it and start depending on the sort. Base64 of JSON is not
// secret, and this test pins only that it is not plainly the key — if it ever
// becomes the raw key, a client will couple to it and the sort can no longer
// change.
func TestCursorIsNotThePlainKey(t *testing.T) {
	key := []string{"2026-09-05T10:00:00Z", "abc"}
	if c := Encode(key); c == key[0] || c == key[0]+","+key[1] {
		t.Fatalf("cursor %q is the sort key in the clear", c)
	}
}

// The load-bearing negative. Treating a bad cursor as "no cursor" silently
// returns page one for a request that asked for page seven.
func TestDecodeRejectsRatherThanRestarts(t *testing.T) {
	for _, bad := range []string{
		"not base64 !!",
		"YWJj",                     // valid base64, not JSON
		"eyJ2Ijo5OSwiayI6WyJhIl19", // {"v":99,"k":["a"]} — wrong version
		"eyJ2IjoxLCJrIjpbXX0",      // {"v":1,"k":[]} — empty key
	} {
		if _, err := Decode(bad); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("Decode(%q) err = %v, want ErrInvalidCursor", bad, err)
		}
	}
}

func TestDecodeEmptyIsFirstPage(t *testing.T) {
	key, err := Decode("")
	if err != nil || key != nil {
		t.Fatalf("Decode(\"\") = %v, %v; want nil, nil", key, err)
	}
}

func TestParseLimits(t *testing.T) {
	if p, err := Parse(0, false, ""); err != nil || p.Limit != DefaultLimit {
		t.Fatalf("default limit = %d, %v; want %d", p.Limit, err, DefaultLimit)
	}
	// Refused, not clamped: a client asking for 10,000 rows must learn it did
	// not get them, rather than silently receiving 200 and assuming the rest
	// do not exist.
	for _, bad := range []int{0, -1, MaxLimit + 1} {
		if _, err := Parse(bad, true, ""); !errors.Is(err, ErrInvalidLimit) {
			t.Errorf("Parse(%d) err = %v, want ErrInvalidLimit", bad, err)
		}
	}
	if p, err := Parse(MaxLimit, true, ""); err != nil || p.Limit != MaxLimit {
		t.Fatalf("Parse(MaxLimit) = %d, %v", p.Limit, err)
	}
}

func TestOfTrimsAndSetsCursor(t *testing.T) {
	p := Params{Limit: 3}
	got := Of(rows(p.Fetch()), p, keyOf) // exactly one more than a page

	if len(got.Items) != 3 {
		t.Fatalf("page held %d items, want 3", len(got.Items))
	}
	if got.NextCursor == "" {
		t.Fatal("a full page with more behind it produced no next cursor")
	}
	// The cursor must name the last row OF THE PAGE, not the extra row that
	// was fetched to detect it. Naming the extra row skips it on the next
	// page — the classic off-by-one in this idiom, and invisible without a
	// test that looks at which key came back.
	key, err := Decode(got.NextCursor)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := keyOf(got.Items[2])
	if key[0] != want[0] || key[1] != want[1] {
		t.Fatalf("next cursor names %v, want the last row of the page %v", key, want)
	}
}

func TestOfLastPageHasNoCursor(t *testing.T) {
	p := Params{Limit: 3}
	if got := Of(rows(3), p, keyOf); got.NextCursor != "" {
		t.Fatalf("an exactly-full final page produced cursor %q, want none", got.NextCursor)
	}
	if got := Of(rows(1), p, keyOf); got.NextCursor != "" {
		t.Fatalf("a short page produced cursor %q, want none", got.NextCursor)
	}
}

// An empty page must marshal as [] rather than null, or every client has to
// handle both.
func TestEmptyPageMarshalsAsArray(t *testing.T) {
	body, err := json.Marshal(Of(nil, Params{Limit: 10}, keyOf))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(body) != `{"items":[],"next_cursor":""}` {
		t.Fatalf("empty page marshalled as %s", body)
	}
}
