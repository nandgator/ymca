//go:build integration && dev

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// may reports whether the GRAPH says the person may record — no API, no
// validity step, nothing between the test and OpenFGA. Revocation is asserted
// here, because through the API the decision-time check would answer first and
// hide a tuple that was never deleted.
func (h *harness) graphAllowsRecording(person, typeID string) bool {
	h.t.Helper()
	ok, err := h.fga.Check(h.ctx, "person:"+person, "may_record", "consumption_type:"+typeID, nil)
	if err != nil {
		h.t.Fatalf("graph check: %v", err)
	}
	return ok
}

func (h *harness) transition(tok, m, verb string, body map[string]any, key string) (int, map[string]any) {
	return h.call(tok, "POST", "/memberships/"+m+"/"+verb, body, "Idempotency-Key", key)
}

// The claim: suspending a membership takes effect NOW, at the graph, with no
// dispatch in between — the first time the application has run 8.3's
// revocation path — and everything that is not coverage survives it.
func TestMembership_SuspensionRevokesCoverageSynchronously(t *testing.T) {
	h := newHarness(t, 0x1a)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	alice := hs.admit(1, "alice")
	h.dispatch()

	record := func(day string) (int, map[string]any) {
		return h.call(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": day}, "Idempotency-Key", hs.key())
	}
	if !h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("precondition: a member on the plan may record")
	}
	if status, out := record(daysAgo(5)); status != http.StatusCreated {
		t.Fatalf("precondition: record = %d %v", status, out)
	}

	t.Run("only an administrator may suspend", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "ARREARS"}, http.StatusForbidden, "forbidden", "Idempotency-Key", hs.key())
	})
	t.Run("the reason is one of the four categories", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "BECAUSE"}, http.StatusBadRequest, "invalid_request", "Idempotency-Key", hs.key())
		h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			nil, http.StatusBadRequest, "invalid_request", "Idempotency-Key", hs.key())
	})

	key := hs.key()
	status, out := h.transition(h.adminTok, alice.membership, "suspend", map[string]any{"reason": "ARREARS"}, key)
	if status != http.StatusOK || out["state"] != "SUSPENDED" {
		t.Fatalf("suspend = %d %v", status, out)
	}

	t.Run("coverage is gone at the graph with no dispatch", func(t *testing.T) {
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("the graph still answers ALLOW after a suspension: the covered tuple was not deleted")
		}
	})
	t.Run("through the API the member is refused", func(t *testing.T) {
		status, _ := record(daysAgo(6))
		if status != http.StatusForbidden {
			t.Fatalf("a suspended member recorded: status %d", status)
		}
	})
	t.Run("the membership and its holder remain", func(t *testing.T) {
		ok, err := h.fga.Check(h.ctx, "person:"+alice.person, "holder", "membership:"+alice.membership, nil)
		if err != nil || !ok {
			t.Fatalf("the holder edge was removed (%v, err %v); suspension lifts coverage only", ok, err)
		}
		if n := h.count(`SELECT count(*) FROM membership_suspension s JOIN membership m ON m.id = s.membership_id
			WHERE m.tenant_id = $1 AND s.reason_category = 'ARREARS' AND s.lifted_at IS NULL
			  AND s.issued_by = $2::uuid`, h.tenantID, h.adminPerson); n != 1 {
			t.Fatalf("%d open suspensions issued by the administrator, want 1", n)
		}
	})
	t.Run("a retried suspension replays, a second is a wrong state", func(t *testing.T) {
		status, again := h.transition(h.adminTok, alice.membership, "suspend", map[string]any{"reason": "ARREARS"}, key)
		if status != http.StatusOK || again["state"] != "SUSPENDED" {
			t.Fatalf("replay = %d %v", status, again)
		}
		h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "CONDUCT"}, http.StatusConflict, "membership_wrong_state",
			"Idempotency-Key", hs.key())
	})
	t.Run("a suspended member still reads their own bills", func(t *testing.T) {
		h.must(alice.tok, "GET", "/me/invoices", nil, http.StatusOK)
		h.must(alice.tok, "GET", "/me/consumption", nil, http.StatusOK)
	})

	t.Run("reinstating restores coverage after the next dispatch, not before", func(t *testing.T) {
		out := h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/reinstate", nil,
			http.StatusOK, "Idempotency-Key", hs.key())
		if out["state"] != "ACTIVE" {
			t.Fatalf("reinstate = %v", out)
		}
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("coverage returned before the dispatcher ran: grants are supposed to lag (8.3)")
		}
		h.dispatch()
		if !h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("coverage did not return after reinstatement and a dispatch")
		}
		if status, _ := record(daysAgo(7)); status != http.StatusCreated {
			t.Fatalf("a reinstated member could not record: %d", status)
		}
		h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/reinstate", nil,
			http.StatusConflict, "membership_wrong_state", "Idempotency-Key", hs.key())
	})
}

// The claim: ADR-101's race, through the real endpoint. A member suspended
// BEFORE the dispatcher ran must not acquire coverage a moment later.
func TestMembership_SuspendingBeforeTheDispatcherRunsLeavesNothingToGrant(t *testing.T) {
	h := newHarness(t, 0x1b)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	h.dispatch() // the configuration, but NOT the member
	alice := hs.admit(1, "alice")

	h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
		map[string]any{"reason": "CONDUCT"}, http.StatusOK, "Idempotency-Key", hs.key())
	h.dispatch()

	if h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("the dispatcher applied a grant that the suspension should have voided")
	}
	if n := h.count(`SELECT 1 WHERE EXISTS (SELECT 1 FROM authorization_outbox
		WHERE fence_object = 'membership_plan:' || (SELECT plan_id::text FROM membership WHERE id = $1::uuid)
		  AND fence_subject = 'membership:' || $1 || '#covered' AND voided_at IS NOT NULL)`, alice.membership); n != 1 {
		t.Fatal("no voided outbox row: the pending grant was not fenced, it just happened to lose a race")
	}
}

// The claim: a revocation that cannot be performed is reported and changes
// nothing — "revocation did not happen and you were told".
func TestMembership_AFailedRevocationRollsBackTheSuspension(t *testing.T) {
	h := newHarness(t, 0x1c)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
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
	status, out := h.transition(h.adminTok, alice.membership, "suspend", map[string]any{"reason": "ARREARS"}, hs.key())
	h.srv = good

	if status != http.StatusInternalServerError {
		t.Fatalf("a failed revocation answered %d %v, want 500", status, out)
	}
	if msg := errMessage(out); msg == "" || msg == "internal error" {
		t.Errorf("the message %q does not say the revocation did not happen", msg)
	}
	if n := h.count(`SELECT count(*) FROM membership WHERE id = $1::uuid AND state = 'ACTIVE'`, alice.membership); n != 1 {
		t.Fatal("the membership is no longer ACTIVE: the state change was not rolled back")
	}
	if n := h.count(`SELECT count(*) FROM membership_suspension s JOIN membership m ON m.id = s.membership_id
		WHERE m.tenant_id = $1`, h.tenantID); n != 0 {
		t.Fatalf("%d suspension rows survive a rolled-back suspension", n)
	}
	if !h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("coverage was removed although the suspension was reported as not done")
	}
	// And the retry, against a working revoker, succeeds: nothing was left half done.
	h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
		map[string]any{"reason": "ARREARS"}, http.StatusOK, "Idempotency-Key", hs.key())
}

type failingRevoker struct{}

func (failingRevoker) DeleteTuples(context.Context, []outbox.Tuple) error {
	return errors.New("openfga is down")
}

// The claim: terminating ends the membership, its obligations, and its place in
// the ACTIVE slot — and nothing can undo it.
func TestMembership_Termination(t *testing.T) {
	h := newHarness(t, 0x1d)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner", "obligates": true, "recurrence": "DAILY"})
	alice := hs.admit(1, "alice")
	h.dispatch()
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
		"amount": "100.00", "currency": "INR", "from": "2020-01-01"}, http.StatusCreated)
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": alice.person, "membership_id": alice.membership,
		"effective_from": daysAgo(10)}, http.StatusCreated)
	// An obligation that has not begun, to be removed rather than shortened.
	future := hs.admit(2, "future")
	h.dispatch()
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": future.person, "membership_id": future.membership,
		"effective_from": daysAgo(-30)}, http.StatusCreated)

	out := h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/terminate", nil,
		http.StatusOK, "Idempotency-Key", hs.key())
	if out["state"] != "TERMINATED" {
		t.Fatalf("terminate = %v", out)
	}
	h.must(h.adminTok, "POST", "/memberships/"+future.membership+"/terminate", nil,
		http.StatusOK, "Idempotency-Key", hs.key())

	t.Run("coverage is gone at the graph", func(t *testing.T) {
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("a terminated member is still covered at the graph")
		}
	})
	t.Run("the obligation ends after today", func(t *testing.T) {
		// Ten days ago through today inclusive: eleven units, not an open-ended debt.
		closed := h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": daysAgo(10), "to": daysAgo(-20)}, http.StatusCreated, "Idempotency-Key", hs.key())
		rows := closed["closed"].([]any)
		if len(rows) != 1 {
			t.Fatalf("closed %d people, want only alice (the not-yet-begun obligation is gone): %v", len(rows), closed)
		}
		if got := rows[0].(map[string]any)["obligated_units"]; got != float64(11) {
			t.Fatalf("obligated_units = %v, want 11: the obligation outlived the membership", got)
		}
	})
	t.Run("terminal: it cannot be reinstated, suspended or terminated again", func(t *testing.T) {
		for _, verb := range []string{"reinstate", "terminate"} {
			h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/"+verb, nil,
				http.StatusConflict, "membership_wrong_state", "Idempotency-Key", hs.key())
		}
		h.refused(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "ARREARS"}, http.StatusConflict, "membership_wrong_state", "Idempotency-Key", hs.key())
	})
	t.Run("the ACTIVE slot is free: the person can be admitted again", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/memberships", map[string]any{
			"person_id": alice.person, "plan_id": hs.planID, "number": "H-1-AGAIN"},
			http.StatusCreated, "Idempotency-Key", hs.key())
	})
	t.Run("an unknown membership is a 404", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/memberships/00000000-0000-0000-0000-00000000dead/terminate", nil,
			http.StatusNotFound, "not_found", "Idempotency-Key", hs.key())
	})
}

// The claim: a membership whose term has ended entitles nobody, with no state
// change and no sweeper — and ONLY entitlement is affected.
func TestMembership_ATermThatEndedEntitlesNobody(t *testing.T) {
	h := newHarness(t, 0x1e)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	alice := hs.admit(1, "alice")
	h.dispatch()
	record := func(day string) int {
		s, _ := h.call(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": day}, "Idempotency-Key", hs.key())
		return s
	}
	if s := record(daysAgo(3)); s != http.StatusCreated {
		t.Fatalf("precondition: %d", s)
	}

	setTerm := func(from, until string) {
		t.Helper()
		if err := h.tx(func(q func(string, ...any)) {
			q(`UPDATE membership SET valid_from = $2::date, valid_until = $3::date WHERE id = $1::uuid`,
				alice.membership, nilIfEmpty(from), nilIfEmpty(until))
		}); err != nil {
			t.Fatal(err)
		}
	}

	setTerm("", daysAgo(1))
	if s := record(daysAgo(4)); s != http.StatusForbidden {
		t.Fatalf("a member whose term ended yesterday recorded: %d", s)
	}
	if !h.graphAllowsRecording(alice.person, dinner) {
		t.Fatal("precondition for this test: the TUPLE is still there, so only the decision-time check can refuse")
	}
	if n := h.count(`SELECT count(*) FROM audit_event WHERE tenant_id = $1
		AND context->>'reason' = 'membership_not_valid'`, h.tenantID); n < 1 {
		t.Fatal("the refusal was not audited with its reason")
	}

	setTerm(daysAgo(-2), "")
	if s := record(daysAgo(5)); s != http.StatusForbidden {
		t.Fatalf("a member whose term has not begun recorded: %d", s)
	}

	setTerm(daysAgo(30), daysAgo(-30))
	if s := record(daysAgo(6)); s != http.StatusCreated {
		t.Fatalf("a member inside their term was refused: %d", s)
	}
	setTerm("", "")
	if s := record(daysAgo(7)); s != http.StatusCreated {
		t.Fatalf("a LIFETIME member (no term) was refused: %d", s)
	}

	t.Run("a principal's own authority does not depend on membership", func(t *testing.T) {
		// The administrator is not a member of anything, and records.
		s, _ := h.call(h.adminTok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": daysAgo(2)}, "Idempotency-Key", hs.key())
		if s != http.StatusCreated {
			t.Fatalf("the administrator was refused: %d", s)
		}
	})
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
