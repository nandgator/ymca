//go:build integration && dev

package main

import (
	"net/http"
	"regexp"
	"testing"
)

var creditNumber = regexp.MustCompile(`^CN/\d{4}-\d{2}/(\d{6})$`)

// The claim (ADR-120): an issued invoice is corrected by a credit note, never
// edited. The note has its own gapless number, reduces what the invoice asks
// for, and moves the invoice to the state the new figures imply — VOIDED when
// nothing is left to ask for and nothing was received, PAID when what was
// received now covers it, and a negative balance (a refund due) when more was
// received than is asked.
func TestCreditNote_CorrectsAnIssuedInvoice(t *testing.T) {
	h := newHarness(t, 0x23)
	hs := h.newHostel()
	breakfast := hs.addType(map[string]any{"name": "Breakfast", "record_mode": "EITHER"})
	bob := hs.admit(1, "bob")
	h.dispatch()
	h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/options", map[string]any{
		"code": "EGG", "name": "Boiled egg", "charge_kind": "USAGE_FEE",
		"amount": "15.00", "currency": "INR"}, http.StatusCreated)

	// invoiceOf makes a fresh issued invoice of 45.00 for bob in the month
	// given, which is closed exactly once per month.
	draftOf := func(month string) string {
		for _, d := range []string{"01", "02", "03"} {
			h.must(bob.tok, "POST", "/consumption-types/"+breakfast+"/records",
				map[string]any{"occurred_on": month + "-" + d, "option_codes": []string{"EGG"}},
				http.StatusCreated, "Idempotency-Key", hs.key())
		}
		closed := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
			map[string]any{"from": month + "-01", "to": month + "-28"},
			http.StatusCreated, "Idempotency-Key", hs.key())
		rows, _ := closed["closed"].([]any)
		if len(rows) != 1 {
			t.Fatalf("closed %d people, want 1: %v", len(rows), closed)
		}
		id := str(rows[0].(map[string]any), "invoice_id")
		h.dispatch()
		return id
	}
	invoiceOf := func(month string) string {
		id := draftOf(month)
		h.must(h.adminTok, "POST", "/invoices/"+id+"/issue", nil, http.StatusOK, "Idempotency-Key", hs.key())
		return id
	}
	credit := func(tok, inv string, body map[string]any, want int) map[string]any {
		t.Helper()
		return h.must(tok, "POST", "/invoices/"+inv+"/credit-notes", body, want, "Idempotency-Key", hs.key())
	}
	get := func(inv string) map[string]any {
		return h.must(h.adminTok, "GET", "/invoices/"+inv, nil, http.StatusOK)
	}

	a := invoiceOf("2025-04")
	t.Run("only the finance manager may credit", func(t *testing.T) {
		h.refused(bob.tok, "POST", "/invoices/"+a+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "x"}, http.StatusForbidden, "forbidden",
			"Idempotency-Key", hs.key())
		auditor := hs.staff(5, "auditor")
		h.grant("principal:"+auditor.princ, "finance_reader", "tenant:"+h.tenantID)
		h.refused(auditor.tok, "POST", "/invoices/"+a+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "x"}, http.StatusForbidden, "forbidden",
			"Idempotency-Key", hs.key())
	})

	t.Run("a partial credit lowers the balance and numbers itself", func(t *testing.T) {
		cn := credit(h.adminTok, a, map[string]any{"amount": "15.00", "reason": "one egg was never served"}, http.StatusCreated)
		m := creditNumber.FindStringSubmatch(str(cn, "number"))
		if m == nil {
			t.Fatalf("credit note number %q is not CN/<fy>/<6 digits>", cn["number"])
		}
		if str(cn, "issued_by_person_id") != h.adminPerson {
			t.Errorf("issued_by_person_id = %v, want the issuing staff person", cn["issued_by_person_id"])
		}
		inv := get(a)
		if inv["state"] != "ISSUED" || inv["balance"] != "30.00" || inv["total_amount"] != "45.00" {
			t.Errorf("after 15 credit: state %v balance %v total %v, want ISSUED 30.00 of 45.00",
				inv["state"], inv["balance"], inv["total_amount"])
		}
		if notes, _ := inv["credit_notes"].([]any); len(notes) != 1 {
			t.Errorf("%d credit notes on the invoice, want 1", len(notes))
		}
		second := credit(h.adminTok, a, map[string]any{"amount": "5.00", "reason": "goodwill"}, http.StatusCreated)
		n1 := creditNumber.FindStringSubmatch(str(second, "number"))
		if n1 == nil || n1[1] <= m[1] {
			t.Errorf("second number %v does not follow the first %v", second["number"], cn["number"])
		}
	})

	t.Run("it cannot credit more than the invoice still asks for", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/invoices/"+a+"/credit-notes",
			map[string]any{"amount": "25.01", "reason": "too much"}, http.StatusConflict,
			"credit_exceeds_invoice", "Idempotency-Key", hs.key())
		credit(h.adminTok, a, map[string]any{"amount": "25.00", "reason": "the rest"}, http.StatusCreated)
		if inv := get(a); inv["state"] != "VOIDED" || inv["balance"] != "0.00" {
			t.Errorf("credited in full with nothing received: state %v balance %v, want VOIDED 0.00",
				inv["state"], inv["balance"])
		}
	})

	t.Run("a voided invoice takes no more credit and no payment", func(t *testing.T) {
		h.refused(h.adminTok, "POST", "/invoices/"+a+"/credit-notes",
			map[string]any{"amount": "1.00", "reason": "again"}, http.StatusConflict,
			"invoice_wrong_state", "Idempotency-Key", hs.key())
		h.refused(h.adminTok, "POST", "/invoices/"+a+"/payments", map[string]any{
			"method": "CASH", "amount": "1.00", "currency": "INR"},
			http.StatusConflict, "invoice_wrong_state", "Idempotency-Key", hs.key())
	})

	b := invoiceOf("2025-05")
	t.Run("a credit counts against what a payment may be", func(t *testing.T) {
		credit(h.adminTok, b, map[string]any{"amount": "15.00", "reason": "one egg"}, http.StatusCreated)
		h.refused(h.adminTok, "POST", "/invoices/"+b+"/payments", map[string]any{
			"method": "CASH", "amount": "30.01", "currency": "INR"},
			http.StatusConflict, "payment_exceeds_balance", "Idempotency-Key", hs.key())
		h.must(h.adminTok, "POST", "/invoices/"+b+"/payments", map[string]any{
			"method": "CASH", "amount": "30.00", "currency": "INR"},
			http.StatusCreated, "Idempotency-Key", hs.key())
		if inv := get(b); inv["state"] != "PAID" || inv["balance"] != "0.00" {
			t.Errorf("paid what remained after a credit: state %v balance %v, want PAID 0.00",
				inv["state"], inv["balance"])
		}
	})

	t.Run("a credit after payment leaves a refund due, and the invoice PAID", func(t *testing.T) {
		credit(h.adminTok, b, map[string]any{"amount": "10.00", "reason": "refund of an overcharge"}, http.StatusCreated)
		if inv := get(b); inv["state"] != "PAID" || inv["balance"] != "-10.00" {
			t.Errorf("state %v balance %v, want PAID -10.00 (a refund due)", inv["state"], inv["balance"])
		}
	})

	c := invoiceOf("2025-06")
	t.Run("a credit that settles what was left makes the invoice PAID", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/invoices/"+c+"/payments", map[string]any{
			"method": "UPI", "amount": "30.00", "currency": "INR"},
			http.StatusCreated, "Idempotency-Key", hs.key())
		credit(h.adminTok, c, map[string]any{"amount": "15.00", "reason": "one egg"}, http.StatusCreated)
		if inv := get(c); inv["state"] != "PAID" || inv["balance"] != "0.00" {
			t.Errorf("state %v balance %v, want PAID 0.00: the credit settled the rest", inv["state"], inv["balance"])
		}
	})

	t.Run("a draft is not yet an invoice to correct", func(t *testing.T) {
		draft := draftOf("2025-08")
		h.refused(h.adminTok, "POST", "/invoices/"+draft+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "x"}, http.StatusConflict,
			"invoice_wrong_state", "Idempotency-Key", hs.key())
	})

	t.Run("the request is validated, and a retry is not a second note", func(t *testing.T) {
		d := invoiceOf("2025-07")
		for _, body := range []map[string]any{
			{"amount": "0", "reason": "x"}, {"amount": "-5.00", "reason": "x"},
			{"amount": "1.234", "reason": "x"}, {"amount": "5.00", "reason": "  "},
			{"amount": "5.00"},
		} {
			h.refused(h.adminTok, "POST", "/invoices/"+d+"/credit-notes", body,
				http.StatusBadRequest, "invalid_request", "Idempotency-Key", hs.key())
		}
		missing := "00000000-0000-0000-0000-000000000000"
		h.refused(h.adminTok, "POST", "/invoices/"+missing+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "x"}, http.StatusNotFound, "not_found",
			"Idempotency-Key", hs.key())

		key := hs.key()
		first := h.must(h.adminTok, "POST", "/invoices/"+d+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "retry"}, http.StatusCreated, "Idempotency-Key", key)
		again := h.must(h.adminTok, "POST", "/invoices/"+d+"/credit-notes",
			map[string]any{"amount": "5.00", "reason": "retry"}, http.StatusCreated, "Idempotency-Key", key)
		if first["id"] != again["id"] {
			t.Errorf("a retry created a second credit note: %v then %v", first["id"], again["id"])
		}
		if n := h.count(`SELECT count(*) FROM credit_note WHERE invoice_id = $1::uuid`, d); n != 1 {
			t.Errorf("%d credit notes after a retry, want 1", n)
		}
	})
}
