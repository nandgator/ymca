//go:build integration && dev

package main

import (
	"net/http"
	"testing"
)

// The claim: a member can be told what they may record without being a tenant
// admin, a stranger is told nothing, and the front desk can find a member by
// name or number — each by the narrowest authority that justifies it.
func TestMe_ConsumptionTypesAreWhatTheCallerMayRecord(t *testing.T) {
	h := newHarness(t, 0x17)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	breakfast := hs.addType(map[string]any{"name": "Breakfast"})
	// Never entitled to the bundle: nobody but a manager may record it.
	h.must(h.adminTok, "POST", "/consumption-types", map[string]any{"name": "Private"}, http.StatusCreated)
	alice := hs.admit(1, "alice")
	stranger := hs.staff(2, "stranger")
	h.dispatch()

	names := func(tok string) []string {
		out := h.must(tok, "GET", "/me/consumption-types", nil, http.StatusOK)
		var got []string
		for _, i := range items(t, out) {
			got = append(got, str(i, "name"))
		}
		return got
	}
	if got := names(alice.tok); len(got) != 2 || got[0] != "Breakfast" || got[1] != "Dinner" {
		t.Errorf("a member sees %v, want [Breakfast Dinner] and not the type nobody entitled them to", got)
	}
	if got := names(stranger.tok); len(got) != 0 {
		t.Errorf("a person with no membership sees %v, want nothing", got)
	}
	if got := names(h.adminTok); len(got) != 3 {
		t.Errorf("the tenant admin sees %v, want all three (manager is inside may_record)", got)
	}
	// The rows are the same ones GET /consumption-types would show an admin.
	_ = dinner
	_ = breakfast

	t.Run("a stranger's listing writes nothing to the audit trail", func(t *testing.T) {
		before := h.count(`SELECT count(*) FROM audit_event WHERE tenant_id = $1`, h.tenantID)
		names(stranger.tok)
		after := h.count(`SELECT count(*) FROM audit_event WHERE tenant_id = $1`, h.tenantID)
		if after != before {
			t.Errorf("listing wrote %d audit rows; visibility is not a decision", after-before)
		}
	})
}

func TestDirectory_TheFrontDeskFindsMembers(t *testing.T) {
	h := newHarness(t, 0x18)
	hs := h.newHostel()
	alice := hs.admit(1, "alice")
	hs.admit(2, "bob")
	hs.admit(3, "carol")
	frontdesk := hs.staff(4, "frontdesk")
	h.grant("principal:"+frontdesk.princ, "may_approve_membership", "tenant:"+h.tenantID)
	h.dispatch()

	t.Run("the member list carries names", func(t *testing.T) {
		out := h.must(frontdesk.tok, "GET", "/memberships", nil, http.StatusOK)
		got := items(t, out)
		if len(got) != 3 {
			t.Fatalf("%d members, want 3", len(got))
		}
		if got[0]["number"] != "H-1" || got[0]["display_name"] != "alice" ||
			got[0]["person_id"] != alice.person {
			t.Errorf("first entry = %v", got[0])
		}
	})
	t.Run("search matches a name fragment or a number prefix", func(t *testing.T) {
		byName := items(t, h.must(frontdesk.tok, "GET", "/memberships?q=aro", nil, http.StatusOK))
		if len(byName) != 1 || byName[0]["display_name"] != "carol" {
			t.Errorf("q=aro found %v, want carol", byName)
		}
		byNumber := items(t, h.must(frontdesk.tok, "GET", "/memberships?q=H-2", nil, http.StatusOK))
		if len(byNumber) != 1 || byNumber[0]["display_name"] != "bob" {
			t.Errorf("q=H-2 found %v, want bob", byNumber)
		}
	})
	t.Run("paging resumes after the last number", func(t *testing.T) {
		first := h.must(frontdesk.tok, "GET", "/memberships?limit=2", nil, http.StatusOK)
		if len(items(t, first)) != 2 || first["next_cursor"] == "" {
			t.Fatalf("first page = %v", first)
		}
		next := items(t, h.must(frontdesk.tok, "GET",
			"/memberships?limit=2&cursor="+first["next_cursor"].(string), nil, http.StatusOK))
		if len(next) != 1 || next[0]["number"] != "H-3" {
			t.Errorf("second page = %v, want only H-3", next)
		}
	})
	t.Run("a plain member may not read the directory", func(t *testing.T) {
		h.refused(alice.tok, "GET", "/memberships", nil, http.StatusForbidden, "forbidden")
	})
}
