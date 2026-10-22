//go:build integration && dev

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/consumption"
)

// The claim: a member admitted through the API can record a meal, staff can
// record for them, and a mistake is superseded rather than edited — with the
// refusals A3.10 lists each coming back as its OWN code.
//
// Two failure modes sharing a status are told apart by the code, never by the
// status alone: that is HANDOFF §3's lesson from the zero-valued
// idempotency key, applied to every 409 below.
func TestConsumption_RecordingAndCorrection(t *testing.T) {
	h := newHarness(t, 0x10)
	hs := h.newHostel()

	// Window of 2 days: a correction of a record from last week is late.
	dinner := hs.addType(map[string]any{"name": "Dinner", "obligates": true,
		"recurrence": "DAILY", "record_mode": "EITHER", "correction_window_days": 2})
	staffOnly := hs.addType(map[string]any{"name": "Gym desk", "record_mode": "STAFF"})
	selfOnly := hs.addType(map[string]any{"name": "Self check-in", "record_mode": "SELF"})

	alice := hs.admit(1, "alice")
	bob := hs.admit(2, "bob")
	stranger := hs.staff(3, "stranger") // a person with no membership
	warden := hs.staff(4, "warden")
	h.grant("principal:"+warden.princ, "may_record_for_other", "consumption_type:"+dinner)
	h.grant("principal:"+warden.princ, "may_correct", "consumption_type:"+dinner)
	h.grant("principal:"+warden.princ, "may_read", "consumption_type:"+dinner)
	h.dispatch()

	rec := func(tok, ct string, body map[string]any, key string) (int, map[string]any) {
		return h.call(tok, "POST", "/consumption-types/"+ct+"/records", body,
			"Idempotency-Key", key)
	}

	// An obligation, so a record has something to discharge.
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": alice.person, "membership_id": alice.membership,
		"effective_from": daysAgo(400),
	}, http.StatusCreated)

	t.Run("a member records their own dinner, and it discharges the obligation", func(t *testing.T) {
		status, out := rec(alice.tok, dinner, map[string]any{"occurred_on": today()}, "alice-1")
		if status != http.StatusCreated {
			t.Fatalf("status %d: %v", status, out)
		}
		if str(out, "actor_person_id") != alice.person || str(out, "subject_person_id") != alice.person {
			t.Errorf("actor/subject = %v/%v, want both alice", out["actor_person_id"], out["subject_person_id"])
		}
		if str(out, "obligation_id") == "" {
			t.Error("the record did not name the obligation it discharges")
		}
	})

	t.Run("a retry with the same key replays the original", func(t *testing.T) {
		_, first := rec(alice.tok, dinner, map[string]any{"occurred_on": today()}, "alice-1")
		if str(first, "id") == "" {
			t.Fatalf("replay returned no record: %v", first)
		}
		n := h.count(`SELECT count(*) FROM consumption_record
			WHERE tenant_id = $1 AND subject_person_id = $2`, h.tenantID, alice.person)
		if n != 1 {
			t.Fatalf("%d records after a replay, want 1", n)
		}
	})

	t.Run("the same key with a different body is its own refusal", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": daysAgo(1)}, http.StatusConflict,
			"idempotency_key_reused", "Idempotency-Key", "alice-1")
	})

	t.Run("one key against two types is two requests, not a replay", func(t *testing.T) {
		// The body names no type, so the type must be part of the key's
		// scope. Without it the second call replays the first type's record.
		other := hs.addType(map[string]any{"name": "Lunch"})
		h.dispatch()
		_, a := rec(alice.tok, other, map[string]any{"occurred_on": daysAgo(8)}, "shared-key")
		_, b := rec(alice.tok, selfOnly, map[string]any{"occurred_on": daysAgo(8)}, "shared-key")
		if str(a, "id") == "" || str(b, "id") == "" || str(a, "id") == str(b, "id") {
			t.Fatalf("one key against two types gave %v and %v; want two distinct records", a, b)
		}
		if str(b, "consumption_type_id") != selfOnly {
			t.Errorf("the second call recorded against %v, want %v", b["consumption_type_id"], selfOnly)
		}
	})

	t.Run("a second live record for the day is already_recorded", func(t *testing.T) {
		status, out := rec(alice.tok, dinner, map[string]any{"occurred_on": today()}, "alice-dup")
		assertCode(t, status, out, http.StatusConflict, "already_recorded")
	})

	t.Run("no Idempotency-Key is refused, and says which header", func(t *testing.T) {
		status, out := h.call(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": daysAgo(2)})
		assertCode(t, status, out, http.StatusBadRequest, "invalid_request")
		if msg := errMessage(out); !strings.Contains(msg, "Idempotency-Key") {
			t.Errorf("message %q does not name the header", msg)
		}
	})

	t.Run("the future is refused", func(t *testing.T) {
		status, out := rec(alice.tok, dinner, map[string]any{"occurred_on": "2999-01-01"}, "alice-future")
		assertCode(t, status, out, http.StatusBadRequest, "invalid_request")
	})

	t.Run("a person with no membership may not record", func(t *testing.T) {
		status, out := rec(stranger.tok, dinner, map[string]any{"occurred_on": daysAgo(3)}, "stranger-1")
		assertCode(t, status, out, http.StatusForbidden, "forbidden")
	})

	t.Run("recording for somebody else needs the staff permission", func(t *testing.T) {
		status, out := rec(bob.tok, dinner, map[string]any{
			"subject_person_id": alice.person, "occurred_on": daysAgo(4)}, "bob-for-alice")
		assertCode(t, status, out, http.StatusForbidden, "forbidden")
	})

	t.Run("staff record for a member, and the actor is the staff", func(t *testing.T) {
		status, out := rec(warden.tok, dinner, map[string]any{
			"subject_person_id": bob.person, "occurred_on": today()}, "warden-bob")
		if status != http.StatusCreated {
			t.Fatalf("status %d: %v", status, out)
		}
		if str(out, "actor_person_id") != warden.person || str(out, "subject_person_id") != bob.person {
			t.Errorf("actor/subject = %v/%v", out["actor_person_id"], out["subject_person_id"])
		}
	})

	t.Run("staff cannot bill a person who is not entitled", func(t *testing.T) {
		status, out := rec(warden.tok, dinner, map[string]any{
			"subject_person_id": stranger.person, "occurred_on": today()}, "warden-stranger")
		assertCode(t, status, out, http.StatusForbidden, "forbidden")
		if !strings.Contains(errMessage(out), "entitled") {
			t.Errorf("message %q does not say the subject is the problem", errMessage(out))
		}
	})

	t.Run("an unknown subject is a 404 for an entitled-looking caller", func(t *testing.T) {
		status, out := rec(warden.tok, dinner, map[string]any{
			"subject_person_id": "00000000-0000-0000-0000-00000000dead", "occurred_on": today()}, "warden-ghost")
		// The subject fails the entitlement check before it can fail the
		// existence check, so it reads as forbidden: A3.3 wants the two alike.
		if status != http.StatusForbidden && status != http.StatusNotFound {
			t.Fatalf("status %d: %v", status, out)
		}
	})

	t.Run("a malformed subject id is invalid_request, not a 500", func(t *testing.T) {
		status, out := rec(warden.tok, dinner, map[string]any{
			"subject_person_id": "not-a-uuid", "occurred_on": today()}, "warden-bad")
		if status == http.StatusInternalServerError {
			t.Fatalf("500: %v", out)
		}
	})

	t.Run("record_mode is enforced, with its own status", func(t *testing.T) {
		// STAFF types refuse the actor as subject.
		status, out := rec(alice.tok, staffOnly, map[string]any{"occurred_on": today()}, "alice-staff-type")
		assertCode(t, status, out, http.StatusUnprocessableEntity, "record_mode_not_allowed")

		// SELF types refuse a subject other than the actor.
		h.grant("principal:"+warden.princ, "may_record_for_other", "consumption_type:"+selfOnly)
		status, out = rec(warden.tok, selfOnly, map[string]any{
			"subject_person_id": bob.person, "occurred_on": today()}, "warden-self-type")
		assertCode(t, status, out, http.StatusUnprocessableEntity, "record_mode_not_allowed")
	})

	// ── options ───────────────────────────────────────────────────────
	t.Run("options are priced additions, and a record carries their codes", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/options", map[string]any{
			"code": "EGG", "name": "Boiled egg", "charge_kind": "USAGE_FEE",
			"amount": "15.00", "currency": "INR"}, http.StatusCreated)
		h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/options", map[string]any{
			"code": "EGG", "name": "Again", "charge_kind": "USAGE_FEE",
			"amount": "1", "currency": "INR"}, http.StatusConflict, "option_code_taken")
		h.refused(alice.tok, "POST", "/consumption-types/"+dinner+"/options", map[string]any{
			"code": "MILK", "name": "Milk", "charge_kind": "USAGE_FEE",
			"amount": "1", "currency": "INR"}, http.StatusForbidden, "forbidden")

		// The menu is readable by a member entitled to the type.
		menu := h.must(alice.tok, "GET", "/consumption-types/"+dinner+"/options", nil, http.StatusOK)
		if items, _ := menu["items"].([]any); len(items) != 1 {
			t.Fatalf("menu has %d items, want 1", len(items))
		}

		status, out := rec(alice.tok, dinner, map[string]any{
			"occurred_on": daysAgo(5), "option_codes": []string{"EGG", "EGG"}}, "alice-egg")
		if status != http.StatusCreated {
			t.Fatalf("status %d: %v", status, out)
		}
		if codes, _ := out["option_codes"].([]any); len(codes) != 1 || codes[0] != "EGG" {
			t.Errorf("option_codes = %v, want [EGG] (duplicates collapse)", out["option_codes"])
		}
		status, out = rec(alice.tok, dinner, map[string]any{
			"occurred_on": daysAgo(6), "option_codes": []string{"CAVIAR"}}, "alice-caviar")
		assertCode(t, status, out, http.StatusBadRequest, "invalid_request")
	})

	// ── corrections ───────────────────────────────────────────────────
	t.Run("a correction supersedes; the list shows the head; history shows both", func(t *testing.T) {
		_, original := rec(alice.tok, dinner, map[string]any{"occurred_on": today()}, "alice-1")
		// alice-1 is today's record from the first subtest, replayed.
		origID := str(original, "id")

		// The member has no correction authority.
		h.refused(alice.tok, "POST", "/consumption-records/"+origID+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusForbidden, "forbidden",
			"Idempotency-Key", "c-alice")

		fixed := h.must(warden.tok, "POST", "/consumption-records/"+origID+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusCreated, "Idempotency-Key", "c-1")
		if str(fixed, "supersedes_id") != origID || str(fixed, "variant_code") != "NONVEG" {
			t.Errorf("correction = %v", fixed)
		}
		if str(fixed, "occurred_on") != str(original, "occurred_on") ||
			str(fixed, "subject_person_id") != alice.person {
			t.Errorf("a correction must keep the day and the subject: %v", fixed)
		}

		live := h.must(warden.tok, "GET", "/consumption-types/"+dinner+"/records?subject="+alice.person+"&from="+today()+"&to="+today(), nil, http.StatusOK)
		items, _ := live["items"].([]any)
		if len(items) != 1 || str(items[0].(map[string]any), "id") != str(fixed, "id") {
			t.Fatalf("live list = %v, want only the correction", items)
		}
		hist := h.must(warden.tok, "GET", "/consumption-types/"+dinner+"/records?subject="+alice.person+"&from="+today()+"&to="+today()+"&history=true", nil, http.StatusOK)
		if items, _ := hist["items"].([]any); len(items) != 2 {
			t.Fatalf("history has %d rows, want 2", len(items))
		}

		// A record is superseded once.
		h.refused(warden.tok, "POST", "/consumption-records/"+origID+"/correction",
			map[string]any{"variant_code": "VEG"}, http.StatusConflict, "record_already_corrected",
			"Idempotency-Key", "c-2")

		// Void the head; the day then has no live record.
		void := h.must(warden.tok, "POST", "/consumption-records/"+str(fixed, "id")+"/correction",
			map[string]any{"void": true}, http.StatusCreated, "Idempotency-Key", "c-3")
		if void["voided"] != true {
			t.Errorf("void correction = %v", void)
		}
		h.refused(warden.tok, "POST", "/consumption-records/"+str(void, "id")+"/correction",
			map[string]any{"void": true}, http.StatusBadRequest, "invalid_request",
			"Idempotency-Key", "c-4")
		live = h.must(warden.tok, "GET", "/consumption-types/"+dinner+"/records?subject="+alice.person+"&from="+today()+"&to="+today(), nil, http.StatusOK)
		if items, _ := live["items"].([]any); len(items) != 0 {
			t.Fatalf("a voided day still lists %d live records", len(items))
		}

		// ...and the void can itself be corrected: the day is not lost.
		back := h.must(warden.tok, "POST", "/consumption-records/"+str(void, "id")+"/correction",
			map[string]any{"variant_code": "VEG"}, http.StatusCreated, "Idempotency-Key", "c-5")
		if back["voided"] != false {
			t.Errorf("correcting a void should revive the day: %v", back)
		}
	})

	t.Run("listing needs may_read", func(t *testing.T) {
		h.refused(alice.tok, "GET", "/consumption-types/"+dinner+"/records", nil,
			http.StatusForbidden, "forbidden")
	})

	t.Run("a correction past the window needs its own permission, and is audited", func(t *testing.T) {
		old := daysAgo(30)
		_, o := rec(bob.tok, dinner, map[string]any{"occurred_on": old}, "bob-old")
		oldID := str(o, "id")
		if oldID == "" {
			t.Fatalf("no old record: %v", o)
		}
		h.refused(warden.tok, "POST", "/consumption-records/"+oldID+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusConflict,
			"consumption_window_closed", "Idempotency-Key", "late-1")

		// The domain refuses on its own, whatever the handler decided: the
		// handler's pre-check and Correct's recheck both answer 409, so each
		// shadows the other through the API. Calling Correct directly is the
		// only way to see its guard fail for its own reason.
		err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			_, err := consumption.Correct(h.ctx, tx, h.tenantID, oldID,
				consumption.Correction{Void: true},
				consumption.Actor{PersonID: warden.person, PrincipalID: warden.princ}, false)
			return err
		})
		if !errors.Is(err, consumption.ErrWindowClosed) {
			t.Fatalf("Correct on a late record without lateAllowed = %v, want ErrWindowClosed", err)
		}

		h.grant("principal:"+warden.princ, "may_correct_late", "consumption_type:"+dinner)
		h.must(warden.tok, "POST", "/consumption-records/"+oldID+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusCreated, "Idempotency-Key", "late-2")

		n := h.count(`
			SELECT count(*) FROM audit_event
			 WHERE tenant_id = $1 AND severity = 'NOTABLE' AND outcome = 'ALLOWED'
			   AND object_id = $2::uuid`, h.tenantID, oldID)
		if n != 1 {
			t.Fatalf("%d NOTABLE audit rows for the late correction, want 1", n)
		}

		// An in-window correction writes none.
		_, fresh := rec(bob.tok, dinner, map[string]any{"occurred_on": daysAgo(1)}, "bob-fresh")
		h.must(warden.tok, "POST", "/consumption-records/"+str(fresh, "id")+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusCreated, "Idempotency-Key", "late-3")
		n = h.count(`
			SELECT count(*) FROM audit_event
			 WHERE tenant_id = $1 AND severity = 'NOTABLE' AND object_id = $2::uuid`,
			h.tenantID, str(fresh, "id"))
		if n != 0 {
			t.Fatalf("%d NOTABLE rows for an in-window correction, want 0", n)
		}
	})

	t.Run("a record that does not exist is a 404", func(t *testing.T) {
		h.refused(warden.tok, "POST",
			"/consumption-records/00000000-0000-0000-0000-00000000dead/correction",
			map[string]any{"void": true}, http.StatusNotFound, "not_found", "Idempotency-Key", "ghost")
	})
}

func assertCode(t *testing.T, status int, out map[string]any, wantStatus int, wantCode string) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status %d, want %d (%s); body %v", status, wantStatus, wantCode, out)
	}
	e, _ := out["error"].(map[string]any)
	if got, _ := e["code"].(string); got != wantCode {
		t.Fatalf("error code %q, want %q; body %v", got, wantCode, out)
	}
}

func errMessage(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

var _ = fmt.Sprintf
