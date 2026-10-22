//go:build integration && dev

package main

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var invoiceNumber = regexp.MustCompile(`^INV/\d{4}-\d{2}/(\d{6})$`)

func numberOf(t *testing.T, inv map[string]any) int {
	t.Helper()
	m := invoiceNumber.FindStringSubmatch(str(inv, "number"))
	if m == nil {
		t.Fatalf("invoice number %q is not INV/<fy>/<6 digits>", str(inv, "number"))
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func items(t *testing.T, page map[string]any) []map[string]any {
	t.Helper()
	raw, _ := page["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(map[string]any))
	}
	return out
}

// The claim: a month of dinners goes from an obligation to a paid invoice
// through the API alone — absences relieve what they say they relieve, the
// close happens exactly once, and an invoice is numbered, paid and replayed
// without ever being created or paid twice.
func TestBilling_FromObligationToPaidInvoice(t *testing.T) {
	h := newHarness(t, 0x11)
	hs := h.newHostel()

	dinner := hs.addType(map[string]any{"name": "Dinner", "obligates": true,
		"recurrence": "DAILY", "record_mode": "EITHER"})
	breakfast := hs.addType(map[string]any{"name": "Breakfast", "record_mode": "EITHER"})

	alice := hs.admit(1, "alice")
	bob := hs.admit(2, "bob")
	stranger := hs.staff(3, "stranger")
	h.dispatch()

	const (
		from = "2026-09-01"
		to   = "2026-09-30"
	)
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": alice.person, "membership_id": alice.membership,
		"effective_from": "2026-01-01"}, http.StatusCreated)
	h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": alice.person, "membership_id": alice.membership,
		"effective_from": "2026-06-01"}, http.StatusConflict, "obligation_overlaps")
	h.refused(h.adminTok, "POST", "/consumption-types/"+breakfast+"/obligations", map[string]any{
		"subject_person_id": bob.person, "membership_id": bob.membership,
		"effective_from": "2026-01-01"}, http.StatusBadRequest, "invalid_request")
	h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": bob.person, "membership_id": alice.membership,
		"effective_from": "2026-01-01"}, http.StatusBadRequest, "invalid_request")

	h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/options", map[string]any{
		"code": "EGG", "name": "Boiled egg", "charge_kind": "USAGE_FEE",
		"amount": "15.00", "currency": "INR"}, http.StatusCreated)

	// ── absences ──────────────────────────────────────────────────────
	var longAbsence string
	t.Run("absences snapshot what they relieve", func(t *testing.T) {
		long := h.must(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-09-10", "to": "2026-09-19"}, http.StatusCreated)
		longAbsence = str(long, "id")
		if long["relieves_payment"] != true || long["relieves_recording"] != true {
			t.Errorf("ten days should relieve both: %v", long)
		}
		short := h.must(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-09-25", "to": "2026-09-27"}, http.StatusCreated)
		if short["relieves_payment"] != false || short["relieves_recording"] != true {
			t.Errorf("three days should relieve recording only: %v", short)
		}
		// Exactly the threshold: seven days relieves payment.
		seven := h.must(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-12-01", "to": "2026-12-07"}, http.StatusCreated)
		if seven["relieves_payment"] != true {
			t.Errorf("seven days is the threshold and should relieve payment: %v", seven)
		}
		six := h.must(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-12-10", "to": "2026-12-15"}, http.StatusCreated)
		if six["relieves_payment"] != false {
			t.Errorf("six days is under the threshold: %v", six)
		}
	})
	t.Run("absence authority", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/absences", map[string]any{
			"subject_person_id": bob.person, "from": "2026-09-01", "to": "2026-09-02"},
			http.StatusForbidden, "forbidden")
		h.refused(stranger.tok, "POST", "/absences",
			map[string]any{"from": "2026-09-01", "to": "2026-09-02"}, http.StatusForbidden, "forbidden")
		h.refused(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-09-05", "to": "2026-09-01"}, http.StatusBadRequest, "invalid_request")
		h.must(h.adminTok, "POST", "/absences", map[string]any{
			"subject_person_id": bob.person, "from": "2026-09-01", "to": "2026-09-02"}, http.StatusCreated)
	})
	t.Run("a withdrawn absence relieves nothing", func(t *testing.T) {
		c := h.must(alice.tok, "POST", "/absences",
			map[string]any{"from": "2026-09-01", "to": "2026-09-08"}, http.StatusCreated)
		h.refused(bob.tok, "POST", "/absences/"+str(c, "id")+"/withdraw", nil,
			http.StatusForbidden, "forbidden")
		h.must(alice.tok, "POST", "/absences/"+str(c, "id")+"/withdraw", nil, http.StatusOK)
		h.must(alice.tok, "POST", "/absences/"+str(c, "id")+"/withdraw", nil, http.StatusOK) // twice is fine
	})

	// ── records the close will see ────────────────────────────────────
	rec := func(tok, ct string, body map[string]any, key string) map[string]any {
		return h.must(tok, "POST", "/consumption-types/"+ct+"/records", body,
			http.StatusCreated, "Idempotency-Key", key)
	}
	aliceDinner := rec(alice.tok, dinner, map[string]any{"occurred_on": "2026-09-05"}, "a-d5")
	rec(bob.tok, breakfast, map[string]any{"occurred_on": "2026-09-05", "option_codes": []string{"EGG"}}, "b-b5")
	rec(bob.tok, breakfast, map[string]any{"occurred_on": "2026-09-06", "option_codes": []string{"EGG"}}, "b-b6")
	voided := rec(bob.tok, breakfast, map[string]any{"occurred_on": "2026-09-07", "option_codes": []string{"EGG"}}, "b-b7")
	h.grant("principal:"+h.adminPrinc, "may_correct", "consumption_type:"+breakfast) // admin already manages; harmless
	h.must(h.adminTok, "POST", "/consumption-records/"+str(voided, "id")+"/correction",
		map[string]any{"void": true}, http.StatusCreated, "Idempotency-Key", "b-void")
	h.dispatch()

	// ── closing without a rate rolls the whole close back ─────────────
	t.Run("units with no rate are refused, and the period stays open", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": from, "to": to}, http.StatusConflict, "no_rate",
			"Idempotency-Key", "close-norate")
		if n := h.count(`SELECT count(*) FROM consumption_period_close WHERE tenant_id = $1`, h.tenantID); n != 0 {
			t.Fatalf("%d close rows survive a refused close", n)
		}
		// Still recordable: the period was not closed.
		rec(alice.tok, dinner, map[string]any{"occurred_on": "2026-09-06"}, "a-d6")
	})

	h.refused(alice.tok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
		"amount": "100.00", "currency": "INR", "from": "2026-01-01"}, http.StatusForbidden, "forbidden")
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
		"amount": "100.00", "currency": "INR", "from": "2026-01-01"}, http.StatusCreated)
	h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
		"amount": "120.00", "currency": "INR", "from": "2026-06-01"}, http.StatusConflict, "rate_overlaps")

	var aliceInvoice, bobInvoice string
	t.Run("closing a month bills units net of relief", func(t *testing.T) {
		out := h.must(alice.tok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": from, "to": to}, http.StatusForbidden, "Idempotency-Key", "x")
		_ = out
		closed := h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": from, "to": to}, http.StatusCreated, "Idempotency-Key", "close-dinner")
		rows, _ := closed["closed"].([]any)
		if len(rows) != 1 {
			t.Fatalf("closed %d people, want 1 (only alice is obligated): %v", len(rows), closed)
		}
		c := rows[0].(map[string]any)
		if c["subject_person_id"] != alice.person {
			t.Errorf("closed %v, want alice", c["subject_person_id"])
		}
		// 30 days, 10 relieved by the long absence. The short absence
		// relieves recording only; the withdrawn one relieves nothing.
		if c["obligated_units"] != float64(20) {
			t.Errorf("obligated_units = %v, want 20", c["obligated_units"])
		}
		aliceInvoice = str(c, "invoice_id")
		if aliceInvoice == "" {
			t.Fatalf("no invoice: %v", c)
		}
		// The invoice's tenant edge is a grant, so it lags a dispatch (8.3):
		// until the dispatcher runs, not even the administrator can read it.
		h.refused(h.adminTok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusForbidden, "forbidden")
		h.dispatch()
		inv := h.must(h.adminTok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusOK)
		if inv["state"] != "DRAFT" || inv["number"] != nil {
			t.Errorf("a new invoice should be an unnumbered DRAFT: %v", inv)
		}
		charges, _ := inv["charges"].([]any)
		if len(charges) != 1 {
			t.Fatalf("%d charges, want 1", len(charges))
		}
		ch := charges[0].(map[string]any)
		if ch["source_type"] != "CONSUMPTION" {
			t.Errorf("source_type = %v, want CONSUMPTION (ADR-100)", ch["source_type"])
		}
		comps, _ := ch["components"].([]any)
		if len(comps) != 1 || comps[0].(map[string]any)["amount"] != "2000.00" ||
			comps[0].(map[string]any)["kind"] != "USAGE_FEE" {
			t.Errorf("components = %v, want one USAGE_FEE of 2000.00", comps)
		}
	})

	t.Run("a non-obligating type bills only its options", func(t *testing.T) {
		closed := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
			map[string]any{"from": from, "to": to}, http.StatusCreated, "Idempotency-Key", "close-breakfast")
		rows, _ := closed["closed"].([]any)
		if len(rows) != 1 {
			t.Fatalf("closed %d people, want 1 (bob): %v", len(rows), closed)
		}
		c := rows[0].(map[string]any)
		if c["obligated_units"] != float64(0) {
			t.Errorf("a non-obligating type has no obligated units: %v", c["obligated_units"])
		}
		bobInvoice = str(c, "invoice_id")
		h.dispatch()
		inv := h.must(h.adminTok, "GET", "/invoices/"+bobInvoice, nil, http.StatusOK)
		comps := inv["charges"].([]any)[0].(map[string]any)["components"].([]any)
		// Two live eggs; the voided third day counts for nothing.
		if len(comps) != 1 || comps[0].(map[string]any)["amount"] != "30.00" {
			t.Errorf("components = %v, want one of 30.00 (2 x 15.00, the voided day excluded)", comps)
		}
		sum := c["summary"].(map[string]any)
		if sum["recorded_units"] != float64(2) {
			t.Errorf("recorded_units = %v, want 2", sum["recorded_units"])
		}
	})

	t.Run("a period closes exactly once", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": "2026-09-15", "to": "2026-10-15"}, http.StatusConflict,
			"period_already_closed", "Idempotency-Key", "close-again")
		// The same key and body is a replay, not a second close.
		again := h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
			map[string]any{"from": from, "to": to}, http.StatusCreated, "Idempotency-Key", "close-dinner")
		if rows, _ := again["closed"].([]any); len(rows) != 1 {
			t.Fatalf("replay returned %v", again)
		}
		if n := h.count(`SELECT count(*) FROM consumption_period_close
			WHERE tenant_id = $1 AND consumption_type_id = $2::uuid`, h.tenantID, dinner); n != 1 {
			t.Fatalf("%d close rows for dinner, want 1", n)
		}
	})

	t.Run("a closed period takes no records and no corrections", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": "2026-09-08"}, http.StatusConflict, "period_closed",
			"Idempotency-Key", "a-d8")
		h.refused(h.adminTok, "POST", "/consumption-records/"+str(aliceDinner, "id")+"/correction",
			map[string]any{"variant_code": "NONVEG"}, http.StatusConflict, "period_closed",
			"Idempotency-Key", "c-closed")
		// A different type's calendar is untouched.
		rec(bob.tok, breakfast, map[string]any{"occurred_on": "2026-10-02"}, "b-oct")
	})

	// ── issue: ADR-103 ────────────────────────────────────────────────
	var firstNumber int
	t.Run("issuing numbers an invoice and freezes it", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/invoices/"+aliceInvoice+"/issue", nil,
			http.StatusForbidden, "forbidden", "Idempotency-Key", "i-alice")
		inv := h.must(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/issue",
			map[string]any{"due_at": "2026-10-31T00:00:00Z"}, http.StatusOK, "Idempotency-Key", "issue-1")
		firstNumber = numberOf(t, inv)
		if inv["state"] != "ISSUED" || inv["total_amount"] != "2000.00" {
			t.Errorf("issued invoice = state %v total %v", inv["state"], inv["total_amount"])
		}
		if !strings.HasPrefix(str(inv, "due_at"), "2026-10-31") {
			t.Errorf("due_at = %v", inv["due_at"])
		}
		// A replay is the original, not a second number.
		replay := h.must(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/issue",
			map[string]any{"due_at": "2026-10-31T00:00:00Z"}, http.StatusOK, "Idempotency-Key", "issue-1")
		if numberOf(t, replay) != firstNumber {
			t.Errorf("a replay allocated a different number")
		}
		h.refused(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/issue", nil,
			http.StatusConflict, "invoice_wrong_state", "Idempotency-Key", "issue-2")

		second := h.must(h.adminTok, "POST", "/invoices/"+bobInvoice+"/issue", nil,
			http.StatusOK, "Idempotency-Key", "issue-bob")
		if got := numberOf(t, second); got != firstNumber+1 {
			t.Errorf("second invoice is number %d, want %d: the series has a gap", got, firstNumber+1)
		}
	})

	// ── payments ──────────────────────────────────────────────────────
	pay := func(tok string, body map[string]any, key string, want int) map[string]any {
		return h.must(tok, "POST", "/invoices/"+aliceInvoice+"/payments", body, want, "Idempotency-Key", key)
	}
	t.Run("payments settle an invoice and never exceed it", func(t *testing.T) {
		h.refused(alice.tok, "POST", "/invoices/"+aliceInvoice+"/payments", map[string]any{
			"method": "CASH", "amount": "10.00", "currency": "INR"},
			http.StatusForbidden, "forbidden", "Idempotency-Key", "p-alice")
		h.refused(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/payments", map[string]any{
			"method": "CARD", "amount": "10.00", "currency": "INR"},
			http.StatusBadRequest, "invalid_request", "Idempotency-Key", "p-card")
		h.refused(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/payments", map[string]any{
			"method": "CASH", "amount": "10.00", "currency": "USD"},
			http.StatusConflict, "currency_mismatch", "Idempotency-Key", "p-usd")
		h.refused(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/payments", map[string]any{
			"method": "CASH", "amount": "2000.01", "currency": "INR"},
			http.StatusConflict, "payment_exceeds_balance", "Idempotency-Key", "p-over")

		first := pay(h.adminTok, map[string]any{"method": "CASH", "amount": "500.00", "currency": "INR"}, "p-1", http.StatusCreated)
		if first["recorded_by"] != h.adminPerson {
			t.Errorf("recorded_by = %v, want the recording staff person", first["recorded_by"])
		}
		// A retried payment is not a second payment.
		again := pay(h.adminTok, map[string]any{"method": "CASH", "amount": "500.00", "currency": "INR"}, "p-1", http.StatusCreated)
		if again["id"] != first["id"] {
			t.Errorf("a retry created a second payment")
		}
		inv := h.must(h.adminTok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusOK)
		if inv["state"] != "ISSUED" || inv["balance"] != "1500.00" {
			t.Errorf("after 500: state %v balance %v, want ISSUED 1500.00", inv["state"], inv["balance"])
		}

		pay(h.adminTok, map[string]any{"method": "UPI", "amount": "1500.00", "currency": "INR"}, "p-2", http.StatusCreated)
		inv = h.must(h.adminTok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusOK)
		if inv["state"] != "PAID" || inv["balance"] != "0.00" {
			t.Errorf("after full payment: state %v balance %v, want PAID 0.00", inv["state"], inv["balance"])
		}
		if ps, _ := inv["payments"].([]any); len(ps) != 2 {
			t.Errorf("%d payments, want 2", len(ps))
		}
		h.refused(h.adminTok, "POST", "/invoices/"+aliceInvoice+"/payments", map[string]any{
			"method": "CASH", "amount": "1.00", "currency": "INR"},
			http.StatusConflict, "invoice_wrong_state", "Idempotency-Key", "p-3")
	})

	// ── reading ───────────────────────────────────────────────────────
	t.Run("reading money is not writing it", func(t *testing.T) {
		auditor := hs.staff(5, "auditor")
		h.grant("principal:"+auditor.princ, "finance_reader", "tenant:"+h.tenantID)
		h.must(auditor.tok, "GET", "/invoices", nil, http.StatusOK)
		h.refused(auditor.tok, "POST", "/invoices/"+bobInvoice+"/payments", map[string]any{
			"method": "CASH", "amount": "1.00", "currency": "INR"},
			http.StatusForbidden, "forbidden", "Idempotency-Key", "aud-pay")
		h.refused(auditor.tok, "POST", "/invoices/"+bobInvoice+"/issue", nil,
			http.StatusForbidden, "forbidden", "Idempotency-Key", "aud-issue")
		h.refused(auditor.tok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
			"amount": "1.00", "currency": "INR", "from": "2030-01-01"},
			http.StatusForbidden, "forbidden")
	})

	t.Run("a payer reads their own invoices and nobody else's", func(t *testing.T) {
		h.dispatch()
		mine := h.must(alice.tok, "GET", "/me/invoices", nil, http.StatusOK)
		got := items(t, mine)
		if len(got) != 1 || got[0]["id"] != aliceInvoice {
			t.Fatalf("/me/invoices = %v, want only alice's issued invoice", got)
		}
		h.must(alice.tok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusOK)
		h.refused(bob.tok, "GET", "/invoices/"+aliceInvoice, nil, http.StatusForbidden, "forbidden")
		h.refused(alice.tok, "GET", "/invoices", nil, http.StatusForbidden, "forbidden")

		all := h.must(h.adminTok, "GET", "/invoices", nil, http.StatusOK)
		if n := len(items(t, all)); n != 2 {
			t.Errorf("tenant list has %d invoices, want 2", n)
		}
	})

	t.Run("a payer is not shown a draft", func(t *testing.T) {
		// Bob's invoice is issued above; make a new draft for alice.
		rec(alice.tok, breakfast, map[string]any{"occurred_on": "2026-10-03"}, "a-b-oct")
		h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/options", map[string]any{
			"code": "MILK", "name": "Milk", "charge_kind": "USAGE_FEE",
			"amount": "20.00", "currency": "INR"}, http.StatusCreated)
		rec(alice.tok, breakfast, map[string]any{"occurred_on": "2026-10-04", "option_codes": []string{"MILK"}}, "a-b-milk")
		closed := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
			map[string]any{"from": "2026-10-01", "to": "2026-10-31"}, http.StatusCreated, "Idempotency-Key", "close-oct")
		var draft string
		for _, c := range closed["closed"].([]any) {
			if c.(map[string]any)["subject_person_id"] == alice.person {
				draft = str(c.(map[string]any), "invoice_id")
			}
		}
		if draft == "" {
			t.Fatalf("no draft for alice: %v", closed)
		}
		h.dispatch()
		h.refused(alice.tok, "GET", "/invoices/"+draft, nil, http.StatusNotFound, "not_found")
		mine := h.must(alice.tok, "GET", "/me/invoices", nil, http.StatusOK)
		for _, i := range items(t, mine) {
			if i["id"] == draft {
				t.Fatal("/me/invoices lists a draft")
			}
		}
		h.must(h.adminTok, "GET", "/invoices/"+draft, nil, http.StatusOK)
	})

	t.Run("/me/consumption is the caller's own, newest first", func(t *testing.T) {
		mine := h.must(alice.tok, "GET", "/me/consumption?limit=2", nil, http.StatusOK)
		got := items(t, mine)
		if len(got) != 2 {
			t.Fatalf("limit=2 returned %d", len(got))
		}
		for _, r := range got {
			if r["subject_person_id"] != alice.person {
				t.Errorf("/me/consumption returned someone else's record: %v", r)
			}
		}
		if got[0]["occurred_on"].(string) < got[1]["occurred_on"].(string) {
			t.Errorf("not newest first: %v then %v", got[0]["occurred_on"], got[1]["occurred_on"])
		}
		if mine["next_cursor"] == "" {
			t.Error("expected a next_cursor: alice has more than two records")
		}
		next := h.must(alice.tok, "GET", "/me/consumption?limit=50&cursor="+mine["next_cursor"].(string), nil, http.StatusOK)
		for _, r := range items(t, next) {
			if r["id"] == got[0]["id"] || r["id"] == got[1]["id"] {
				t.Errorf("the second page repeats %v", r["id"])
			}
		}
	})

	_ = longAbsence
}
