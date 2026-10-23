//go:build integration && dev

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The claim: the Flutter client and the server agree on the wire.
//
// This boots the real routes, seeds a hostel with a draft, a part-paid and a
// paid invoice, and runs app/test/live_api_test.dart against it. The widget
// tests use a fake that agrees with the client by construction, so only this
// can catch a model reading a key the server does not write — or a path the
// client spells differently.
//
// Skipped when `flutter` is not on PATH, and says so; CI installs it.
func TestFlutterClientAgreesWithTheServer(t *testing.T) {
	flutter, err := exec.LookPath("flutter")
	if err != nil {
		// CI sets YMCA_REQUIRE_FLUTTER so that a runner without Flutter fails
		// instead of quietly dropping the only test that checks the client
		// against the server.
		if os.Getenv("YMCA_REQUIRE_FLUTTER") != "" {
			t.Fatal("flutter is not on PATH and YMCA_REQUIRE_FLUTTER is set")
		}
		t.Skip("flutter is not on PATH; the Dart half of the contract test did not run")
	}

	h := newHarness(t, 0x19)
	// A tenant that is not at UTC, so the Dart half proves it reads the calendar.
	h.seed(`UPDATE tenant SET time_zone = 'Asia/Kolkata' WHERE id = $1`, h.tenantID)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner", "obligates": true, "recurrence": "DAILY"})
	breakfast := hs.addType(map[string]any{"name": "Breakfast"})
	alice := hs.admit(1, "alice")
	bob := hs.admit(2, "bob")
	carol := hs.admit(4, "carol") // the lifecycle test's member
	desk := hs.staff(3, "desk")
	h.grant("principal:"+desk.princ, "may_approve_membership", "tenant:"+h.tenantID)
	h.grant("principal:"+desk.princ, "may_register_person", "tenant:"+h.tenantID)
	h.dispatch()

	h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/options", map[string]any{
		"code": "EGG", "name": "Boiled egg", "charge_kind": "USAGE_FEE",
		"amount": "15.00", "currency": "INR"}, http.StatusCreated)
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/rates", map[string]any{
		"amount": "100.00", "currency": "INR", "from": "2026-01-01"}, http.StatusCreated)
	h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
		"subject_person_id": alice.person, "membership_id": alice.membership,
		"effective_from": "2026-01-01"}, http.StatusCreated)
	h.must(alice.tok, "POST", "/absences",
		map[string]any{"from": "2026-09-10", "to": "2026-09-19"}, http.StatusCreated)

	record := func(tok, ct, day, key string, options ...string) {
		body := map[string]any{"occurred_on": day}
		if len(options) > 0 {
			body["option_codes"] = options
		}
		h.must(tok, "POST", "/consumption-types/"+ct+"/records", body,
			http.StatusCreated, "Idempotency-Key", key)
	}
	record(bob.tok, breakfast, "2026-09-05", "seed-b1", "EGG")
	record(alice.tok, breakfast, "2026-10-02", "seed-a1", "EGG")
	record(bob.tok, breakfast, "2026-10-03", "seed-b2", "EGG")

	closeDinner := h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/close",
		map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, http.StatusCreated,
		"Idempotency-Key", "seed-close-dinner")
	closeSept := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
		map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, http.StatusCreated,
		"Idempotency-Key", "seed-close-bfast-sep")

	invoiceFor := func(closed map[string]any, person string) string {
		for _, c := range closed["closed"].([]any) {
			m := c.(map[string]any)
			if m["subject_person_id"] == person {
				return str(m, "invoice_id")
			}
		}
		t.Fatalf("no closed period for %s in %v", person, closed)
		return ""
	}
	issued := invoiceFor(closeDinner, alice.person)
	paid := invoiceFor(closeSept, bob.person)

	// Issue and pay BEFORE closing October: a close appends to the payer's
	// open draft, so closing first would put October's charge on the invoice
	// being issued. That is the design (A3.11), and seeding must respect it.
	h.dispatch()
	h.must(h.adminTok, "POST", "/invoices/"+issued+"/issue", nil, http.StatusOK, "Idempotency-Key", "seed-i1")
	h.must(h.adminTok, "POST", "/invoices/"+issued+"/payments", map[string]any{
		"method": "CASH", "amount": "500.00", "currency": "INR"}, http.StatusCreated, "Idempotency-Key", "seed-p1")
	h.must(h.adminTok, "POST", "/invoices/"+paid+"/issue", nil, http.StatusOK, "Idempotency-Key", "seed-i2")
	h.must(h.adminTok, "POST", "/invoices/"+paid+"/payments", map[string]any{
		"method": "UPI", "amount": "15.00", "currency": "INR"}, http.StatusCreated, "Idempotency-Key", "seed-p2")

	closeOct := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
		map[string]any{"from": "2026-10-01", "to": "2026-10-15"}, http.StatusCreated,
		"Idempotency-Key", "seed-close-bfast-oct")
	h.dispatch()

	draft := invoiceFor(closeOct, alice.person)
	forIssue := invoiceFor(closeOct, bob.person)

	closeJSON, err := json.Marshal(closeDinner)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, flutter, "test", "test/live_api_test.dart", "--reporter", "expanded")
	cmd.Dir = "../../../app"
	cmd.Env = append(os.Environ(),
		"YMCA_URL="+h.srv.URL,
		"YMCA_TENANT="+h.tenantID,
		"YMCA_ADMIN_TOKEN="+h.adminTok,
		"YMCA_ALICE_TOKEN="+alice.tok,
		"YMCA_DESK_TOKEN="+desk.tok,
		"YMCA_ALICE_PERSON="+alice.person,
		"YMCA_CAROL_MEMBERSHIP="+carol.membership,
		"YMCA_DINNER_ID="+dinner,
		"YMCA_BREAKFAST_ID="+breakfast,
		"YMCA_INV_ISSUED="+issued,
		"YMCA_INV_PAID="+paid,
		"YMCA_INV_DRAFT="+draft,
		"YMCA_INV_FOR_ISSUE="+forIssue,
		"YMCA_CLOSE_JSON="+string(closeJSON),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the Flutter client disagrees with the server: %v\n%s", err, out)
	}
	t.Logf("flutter: %s", lastLines(string(out), 3))
}

func lastLines(s string, n int) string {
	lines := []string{}
	cur := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	lines = append(lines, cur)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
