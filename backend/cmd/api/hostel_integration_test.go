//go:build integration && dev

package main

import (
	"fmt"
	"net/http"
	"time"
)

// hostel is the configuration A3.10 and A3.11 tests need: a plan whose bundle
// entitles every type added through addType, and members admitted onto it —
// all through the API, with nothing but the administrator's authority written
// by hand.
type hostel struct {
	h        *harness
	bundleID string
	planID   string
	n        int // idempotency key and membership number counter
}

func (h *harness) newHostel() *hostel {
	h.t.Helper()
	b := h.must(h.adminTok, "POST", "/entitlement-bundles",
		map[string]any{"name": "Mess"}, http.StatusCreated)
	hs := &hostel{h: h, bundleID: str(b, "id")}
	p := h.must(h.adminTok, "POST", "/membership-plans", map[string]any{
		"code": "resident", "name": "Resident", "acquisition": "PURCHASED",
		"duration": "ANNUAL", "entitlement_bundle_id": hs.bundleID,
	}, http.StatusCreated)
	hs.planID = str(p, "id")
	return hs
}

func (hs *hostel) key() string {
	hs.n++
	return fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), hs.n)
}

// addType defines a consumption type and entitles the bundle to it.
func (hs *hostel) addType(body map[string]any) string {
	hs.h.t.Helper()
	ct := hs.h.must(hs.h.adminTok, "POST", "/consumption-types", body, http.StatusCreated)
	id := str(ct, "id")
	hs.h.must(hs.h.adminTok, "POST", "/entitlement-bundles/"+hs.bundleID+"/entitles",
		map[string]any{"object_type": "consumption_type", "object_id": id}, http.StatusNoContent)
	return id
}

type member struct {
	person, princ, tok, membership string
}

// admit creates a person and a login for them and admits them onto the plan
// through the API. i must be unique within the test.
func (hs *hostel) admit(i int, name string) member {
	hs.h.t.Helper()
	h := hs.h
	m := member{
		person: fmt.Sprintf("e0000000-0000-0000-0001-1%03x%08x", i, h.tenantSuffix()),
		princ:  fmt.Sprintf("e0000000-0000-0000-0002-1%03x%08x", i, h.tenantSuffix()),
	}
	m.tok = h.newLogin(m.person, m.princ, fmt.Sprintf("%s-%d-%s", name, i, h.tenantID), "PERSONAL", name)
	adm := h.must(h.adminTok, "POST", "/memberships", map[string]any{
		"person_id": m.person, "plan_id": hs.planID, "number": fmt.Sprintf("H-%d", i),
	}, http.StatusCreated, "Idempotency-Key", hs.key())
	m.membership = str(adm, "id")
	return m
}

// staff creates a person and a STAFF login with no authority at all; the test
// grants what it is about.
func (hs *hostel) staff(i int, name string) member {
	hs.h.t.Helper()
	h := hs.h
	m := member{
		person: fmt.Sprintf("e0000000-0000-0000-0001-2%03x%08x", i, h.tenantSuffix()),
		princ:  fmt.Sprintf("e0000000-0000-0000-0002-2%03x%08x", i, h.tenantSuffix()),
	}
	m.tok = h.newLogin(m.person, m.princ, fmt.Sprintf("%s-%d-%s", name, i, h.tenantID), "STAFF", name)
	return m
}

// tenantSuffix is the last bytes of the tenant id as a number, so ids made for
// two tests never collide.
func (h *harness) tenantSuffix() int {
	var n int
	fmt.Sscanf(h.tenantID[len(h.tenantID)-2:], "%x", &n)
	return n
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func daysAgo(n int) string { return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02") }
