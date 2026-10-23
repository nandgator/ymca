//go:build integration && dev

package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/httpx"
)

// planWith makes a second plan of the tenant, granting a bundle of its own that
// entitles one consumption type, so that moving a member onto it changes what
// they may record.
func (hs *hostel) planWith(code, typeName string) (planID, typeID string) {
	hs.h.t.Helper()
	b := hs.h.must(hs.h.adminTok, "POST", "/entitlement-bundles",
		map[string]any{"name": code}, http.StatusCreated)
	ct := hs.h.must(hs.h.adminTok, "POST", "/consumption-types",
		map[string]any{"name": typeName}, http.StatusCreated)
	typeID = str(ct, "id")
	hs.h.must(hs.h.adminTok, "POST", "/entitlement-bundles/"+str(b, "id")+"/entitles",
		map[string]any{"object_type": "consumption_type", "object_id": typeID}, http.StatusNoContent)
	p := hs.h.must(hs.h.adminTok, "POST", "/membership-plans", map[string]any{
		"code": code, "name": code, "acquisition": "PURCHASED", "duration": "ANNUAL",
		"entitlement_bundle_id": str(b, "id"),
	}, http.StatusCreated)
	return str(p, "id"), typeID
}

// The claim (ADR-121): a plan is closed to newcomers, replaced by a successor
// and retired when empty, never edited while people hold it — and each step
// is refused from any state it does not belong to, with its own code.
func TestPlanLifecycle_CloseSupersedeRetire(t *testing.T) {
	h := newHarness(t, 0x24)
	hs := h.newHostel()
	old := hs.planID
	next, _ := hs.planWith("resident-2", "Lunch")
	alice := hs.admit(1, "alice")
	h.dispatch()

	act := func(tok, plan, verb string, body map[string]any) (int, map[string]any) {
		return h.call(tok, "POST", "/membership-plans/"+plan+"/"+verb, body, "Idempotency-Key", hs.key())
	}
	wrong := func(plan, verb string, body map[string]any, code string) {
		t.Helper()
		h.refused(h.adminTok, "POST", "/membership-plans/"+plan+"/"+verb, body,
			http.StatusConflict, code, "Idempotency-Key", hs.key())
	}

	t.Run("only an administrator may change a plan", func(t *testing.T) {
		for _, verb := range []string{"close", "reopen", "retire", "supersede"} {
			if s, _ := act(alice.tok, old, verb, map[string]any{"successor_plan_id": next}); s != http.StatusForbidden {
				t.Errorf("a member could %s a plan: %d", verb, s)
			}
		}
	})

	t.Run("closing stops admission and keeps the holders", func(t *testing.T) {
		out := h.must(h.adminTok, "POST", "/membership-plans/"+old+"/close", nil, http.StatusOK,
			"Idempotency-Key", hs.key())
		if out["status"] != "CLOSED_TO_NEW" {
			t.Fatalf("status = %v", out["status"])
		}
		person := "e0000000-0000-0000-0001-3001000000" + "24"
		h.seed(`INSERT INTO person (id, display_name, status) VALUES ($1, 'newcomer', 'ACTIVE')`, person)
		h.persons = append(h.persons, person) // so the cleanup removes it
		h.refused(h.adminTok, "POST", "/memberships", map[string]any{
			"person_id": person, "plan_id": old, "number": "NEW-1"},
			http.StatusBadRequest, "invalid_request", "Idempotency-Key", hs.key())
		wrong(old, "close", nil, "plan_wrong_state")
	})

	t.Run("a retry of the same close is the same answer, not plan_wrong_state", func(t *testing.T) {
		key := hs.key()
		a := h.must(h.adminTok, "POST", "/membership-plans/"+next+"/close", nil, http.StatusOK, "Idempotency-Key", key)
		b := h.must(h.adminTok, "POST", "/membership-plans/"+next+"/close", nil, http.StatusOK, "Idempotency-Key", key)
		if a["status"] != "CLOSED_TO_NEW" || b["status"] != "CLOSED_TO_NEW" {
			t.Errorf("close and its replay = %v / %v", a["status"], b["status"])
		}
		h.must(h.adminTok, "POST", "/membership-plans/"+next+"/reopen", nil, http.StatusOK, "Idempotency-Key", hs.key())
		wrong(next, "reopen", nil, "plan_wrong_state")
	})

	t.Run("a successor must be another, open, unreplaced plan of the tenant", func(t *testing.T) {
		for _, body := range []map[string]any{
			{"successor_plan_id": old},
			{},
			{"successor_plan_id": "00000000-0000-0000-0000-000000000000"},
			{"successor_plan_id": "plan-2"},
		} {
			h.refused(h.adminTok, "POST", "/membership-plans/"+old+"/supersede", body,
				http.StatusBadRequest, "invalid_request", "Idempotency-Key", hs.key())
		}
		closed, _ := hs.planWith("closed-one", "Brunch")
		h.must(h.adminTok, "POST", "/membership-plans/"+closed+"/close", nil, http.StatusOK, "Idempotency-Key", hs.key())
		h.refused(h.adminTok, "POST", "/membership-plans/"+old+"/supersede",
			map[string]any{"successor_plan_id": closed}, http.StatusBadRequest, "invalid_request",
			"Idempotency-Key", hs.key())
	})

	t.Run("superseding names the successor and the plan stays closed", func(t *testing.T) {
		out := h.must(h.adminTok, "POST", "/membership-plans/"+old+"/supersede",
			map[string]any{"successor_plan_id": next}, http.StatusOK, "Idempotency-Key", hs.key())
		if out["superseded_by_id"] != next || out["status"] != "CLOSED_TO_NEW" {
			t.Fatalf("superseded plan = %v", out)
		}
		wrong(old, "supersede", map[string]any{"successor_plan_id": next}, "plan_wrong_state")
		wrong(old, "reopen", nil, "plan_wrong_state")
		// The chain cannot loop: the replaced plan cannot be a successor.
		h.refused(h.adminTok, "POST", "/membership-plans/"+next+"/supersede",
			map[string]any{"successor_plan_id": old}, http.StatusBadRequest, "invalid_request",
			"Idempotency-Key", hs.key())
		items := h.must(h.adminTok, "GET", "/membership-plans", nil, http.StatusOK)["items"].([]any)
		var seen bool
		for _, it := range items {
			if m := it.(map[string]any); m["id"] == old {
				seen = m["superseded_by_id"] == next
			}
		}
		if !seen {
			t.Error("the plan list does not show superseded_by_id")
		}
	})

	t.Run("a plan with holders is not retired, and an open one is not either", func(t *testing.T) {
		wrong(old, "retire", nil, "plan_has_holders")
		wrong(next, "retire", nil, "plan_wrong_state")
	})

	t.Run("once the holders move, the plan retires and admits nobody", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/plan",
			map[string]any{"plan_id": next}, http.StatusOK, "Idempotency-Key", hs.key())
		out := h.must(h.adminTok, "POST", "/membership-plans/"+old+"/retire", nil, http.StatusOK,
			"Idempotency-Key", hs.key())
		if out["status"] != "RETIRED" {
			t.Fatalf("status = %v", out["status"])
		}
		wrong(old, "retire", nil, "plan_wrong_state")
	})

	t.Run("a terminated member does not hold a plan", func(t *testing.T) {
		third, _ := hs.planWith("resident-3", "Tea")
		bob := hs.admitOn(third, 2, "bob")
		h.must(h.adminTok, "POST", "/membership-plans/"+third+"/close", nil, http.StatusOK, "Idempotency-Key", hs.key())
		wrong(third, "retire", nil, "plan_has_holders")
		h.must(h.adminTok, "POST", "/memberships/"+bob.membership+"/terminate", nil, http.StatusOK, "Idempotency-Key", hs.key())
		h.must(h.adminTok, "POST", "/membership-plans/"+third+"/retire", nil, http.StatusOK, "Idempotency-Key", hs.key())
	})
}

// The claim (ADR-121): moving a member is per-membership and auditable, takes
// effect at the graph at once for what the member LEAVES, and lags a dispatch
// for what they join — never the other way round.
func TestPlanLifecycle_MovingAMember(t *testing.T) {
	h := newHarness(t, 0x25)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"}) // on the first plan's bundle
	next, lunch := hs.planWith("resident-2", "Lunch")
	alice := hs.admit(1, "alice")
	h.dispatch()
	move := func(tok, m string, body map[string]any) (int, map[string]any) {
		return h.call(tok, "POST", "/memberships/"+m+"/plan", body, "Idempotency-Key", hs.key())
	}

	t.Run("only an administrator may move a member", func(t *testing.T) {
		if s, _ := move(alice.tok, alice.membership, map[string]any{"plan_id": next}); s != http.StatusForbidden {
			t.Errorf("a member moved themselves: %d", s)
		}
	})

	t.Run("the target must be another, open plan of the tenant", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"the same plan":  {"plan_id": hs.planID},
			"no plan":        {},
			"a missing plan": {"plan_id": "00000000-0000-0000-0000-000000000000"},
		} {
			if s, out := move(h.adminTok, alice.membership, body); s != http.StatusBadRequest {
				t.Errorf("%s: %d %v, want 400", name, s, out)
			}
		}
		closed, _ := hs.planWith("closed-one", "Brunch")
		h.must(h.adminTok, "POST", "/membership-plans/"+closed+"/close", nil, http.StatusOK, "Idempotency-Key", hs.key())
		if s, out := move(h.adminTok, alice.membership, map[string]any{"plan_id": closed}); s != http.StatusBadRequest {
			t.Errorf("a closed target: %d %v, want 400", s, out)
		}
		if n := h.count(`SELECT count(*) FROM membership_plan_change WHERE tenant_id = $1`, h.tenantID); n != 0 {
			t.Errorf("%d plan changes recorded for refused moves", n)
		}
	})

	if !h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("precondition: alice may record dinner on her first plan")
	}
	t.Run("what the member leaves is revoked at once; what they join lags a dispatch", func(t *testing.T) {
		out := h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/plan",
			map[string]any{"plan_id": next}, http.StatusOK, "Idempotency-Key", hs.key())
		if out["plan_id"] != next {
			t.Fatalf("plan_id = %v, want %s", out["plan_id"], next)
		}
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Error("the old plan's coverage survived the move: the graph still lets alice record dinner")
		}
		if h.graphAllowsRecording(alice.person, lunch) {
			t.Error("the new plan's coverage was granted before the dispatcher ran")
		}
		h.dispatch()
		if !h.graphAllowsRecording(alice.person, lunch) {
			t.Error("after the dispatch alice may not record lunch on her new plan")
		}
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Error("a dispatch brought the old coverage back")
		}
	})

	t.Run("the move is audited", func(t *testing.T) {
		if n := h.count(`SELECT count(*) FROM membership_plan_change
			WHERE tenant_id = $1 AND membership_id = $2::uuid AND from_plan_id = $3::uuid
			  AND to_plan_id = $4::uuid AND changed_by_person_id = $5::uuid`,
			h.tenantID, alice.membership, hs.planID, next, h.adminPerson); n != 1 {
			t.Errorf("%d audit rows for the move, want 1", n)
		}
	})

	t.Run("a suspended member changes plan and is covered by the new one on reinstating", func(t *testing.T) {
		third, thirdType := hs.planWith("resident-3", "Tea")
		h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "ARREARS"}, http.StatusOK, "Idempotency-Key", hs.key())
		h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/plan",
			map[string]any{"plan_id": third}, http.StatusOK, "Idempotency-Key", hs.key())
		h.dispatch()
		if h.graphAllowsRecording(alice.person, thirdType) {
			t.Error("a suspended member was covered by the plan they were moved to")
		}
		h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/reinstate", nil, http.StatusOK,
			"Idempotency-Key", hs.key())
		h.dispatch()
		if !h.graphAllowsRecording(alice.person, thirdType) {
			t.Error("reinstated, alice is not covered by the plan she was moved to")
		}
		if h.graphAllowsRecording(alice.person, lunch) {
			t.Error("reinstated, alice is covered by the plan she left")
		}
	})

	t.Run("a grant still pending when the member moves is voided, not applied", func(t *testing.T) {
		carol := hs.admit(3, "carol") // admitted and NOT dispatched: the old grant is still a pending row
		h.must(h.adminTok, "POST", "/memberships/"+carol.membership+"/plan",
			map[string]any{"plan_id": next}, http.StatusOK, "Idempotency-Key", hs.key())
		h.dispatch()
		if h.graphAllowsRecording(carol.person, dinner) {
			t.Error("the dispatcher applied a grant for the plan the member had already left")
		}
		if n := h.count(`SELECT count(*) FROM authorization_outbox
			WHERE fence_subject = 'membership:' || $1 || '#covered'
			  AND fence_object = 'membership_plan:' || $2 AND voided_at IS NOT NULL`,
			carol.membership, hs.planID); n != 1 {
			t.Errorf("%d voided rows for the old grant, want 1: it was not fenced, it just lost a race", n)
		}
		if !h.graphAllowsRecording(carol.person, lunch) {
			t.Error("carol is not covered by the plan she moved to")
		}
	})

	t.Run("a terminated membership cannot be moved", func(t *testing.T) {
		bob := hs.admit(2, "bob")
		h.must(h.adminTok, "POST", "/memberships/"+bob.membership+"/terminate", nil, http.StatusOK, "Idempotency-Key", hs.key())
		h.refused(h.adminTok, "POST", "/memberships/"+bob.membership+"/plan",
			map[string]any{"plan_id": next}, http.StatusConflict, "membership_wrong_state",
			"Idempotency-Key", hs.key())
	})
}

// The claim: a revocation that cannot be performed changes nothing, for a move
// as for a suspension.
func TestPlanLifecycle_AFailedRevocationRollsBackTheMove(t *testing.T) {
	h := newHarness(t, 0x26)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	next, _ := hs.planWith("resident-2", "Lunch")
	alice := hs.admit(1, "alice")
	h.dispatch()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authenticator, err := auth.Open("dev", auth.Deps{DB: h.pool, DevAuthSecret: h.cfg.DevAuthSecret})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerRoutes(mux, h.pool, h.fga, failingRevoker{}, logger)
	broken := httptest.NewServer(httpx.Chain(mux, httpx.Recover(logger), httpx.RequestID(),
		httpx.Logging(logger), httpx.Authenticate(authenticator, logger)))
	defer broken.Close()
	good := h.srv
	h.srv = broken
	status, out := h.call(h.adminTok, "POST", "/memberships/"+alice.membership+"/plan",
		map[string]any{"plan_id": next}, "Idempotency-Key", hs.key())
	h.srv = good

	if status != http.StatusInternalServerError {
		t.Fatalf("a failed revocation answered %d %v, want 500", status, out)
	}
	if n := h.count(`SELECT count(*) FROM membership WHERE id = $1::uuid AND plan_id = $2::uuid`,
		alice.membership, hs.planID); n != 1 {
		t.Fatal("the membership is on another plan: the move was not rolled back")
	}
	if n := h.count(`SELECT count(*) FROM membership_plan_change WHERE tenant_id = $1`, h.tenantID); n != 0 {
		t.Fatalf("%d plan changes survive a rolled-back move", n)
	}
	if !h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("coverage was removed although the move was reported as not done")
	}
}
