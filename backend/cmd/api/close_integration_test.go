//go:build integration && dev

package main

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/finance"
)

// The claim: obligated units follow the type's recurrence over the
// intersection of the obligation and the period — nothing is materialized, so
// the arithmetic is the whole story and is worth pinning to numbers.
func TestClose_UnitsFollowRecurrence(t *testing.T) {
	h := newHarness(t, 0x12)
	hs := h.newHostel()
	daily := hs.addType(map[string]any{"name": "Daily", "obligates": true, "recurrence": "DAILY"})
	weekly := hs.addType(map[string]any{"name": "Weekly", "obligates": true, "recurrence": "WEEKLY"})
	perOcc := hs.addType(map[string]any{"name": "Per occurrence", "obligates": true, "recurrence": "PER_OCCURRENCE"})
	alice := hs.admit(1, "alice")
	h.dispatch()

	for _, ct := range []string{daily, weekly, perOcc} {
		h.must(h.adminTok, "POST", "/consumption-types/"+ct+"/rates", map[string]any{
			"amount": "10.00", "currency": "INR", "from": "2026-01-01"}, http.StatusCreated)
	}
	// Obligated from Thursday 10 Sep to Sunday 20 Sep inclusive: eleven days,
	// in the ISO weeks of Mon 7 and Mon 14.
	for _, ct := range []string{daily, weekly} {
		h.must(h.adminTok, "POST", "/consumption-types/"+ct+"/obligations", map[string]any{
			"subject_person_id": alice.person, "membership_id": alice.membership,
			"effective_from": "2026-09-10", "effective_until": "2026-09-20"}, http.StatusCreated)
	}
	for i, d := range []string{"2026-09-02", "2026-09-03", "2026-09-04"} {
		h.must(alice.tok, "POST", "/consumption-types/"+perOcc+"/records",
			map[string]any{"occurred_on": d}, http.StatusCreated, "Idempotency-Key", "po-"+string(rune('a'+i)))
	}

	units := func(ct string) float64 {
		out := h.must(h.adminTok, "POST", "/consumption-types/"+ct+"/close",
			map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, http.StatusCreated,
			"Idempotency-Key", "close-"+ct)
		rows := out["closed"].([]any)
		if len(rows) != 1 {
			t.Fatalf("closed %d people for %s, want 1", len(rows), ct)
		}
		return rows[0].(map[string]any)["obligated_units"].(float64)
	}
	if got := units(daily); got != 11 {
		t.Errorf("DAILY units = %v, want 11 (10 to 20 September inclusive)", got)
	}
	if got := units(weekly); got != 2 {
		t.Errorf("WEEKLY units = %v, want 2 (the weeks of 7 and 14 September)", got)
	}
	if got := units(perOcc); got != 3 {
		t.Errorf("PER_OCCURRENCE units = %v, want 3 (what was recorded)", got)
	}
}

// The claim: closing and recording serialise on the (type, subject) lock, so
// a record cannot commit between the close reading the period and writing its
// row and be neither billed nor refused.
func TestClose_WaitsForTheSubjectLock(t *testing.T) {
	h := newHarness(t, 0x13)
	hs := h.newHostel()
	breakfast := hs.addType(map[string]any{"name": "Breakfast"})
	alice := hs.admit(1, "alice")
	h.dispatch()
	h.must(alice.tok, "POST", "/consumption-types/"+breakfast+"/records",
		map[string]any{"occurred_on": "2026-09-05"}, http.StatusCreated, "Idempotency-Key", "k1")

	held, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseLock := func() { releaseOnce.Do(func() { close(release) }) }
	// Registered after the harness, so it runs BEFORE the harness closes its
	// pool: a failing assertion below must not leave the holder's transaction
	// open, because pool.Close waits for every connection and would hang the
	// run. Found by removing the lock on purpose, which made this fail the
	// way it should and then hang for ten minutes.
	t.Cleanup(releaseLock)
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			if err := consumption.LockSubject(h.ctx, tx, breakfast, alice.person); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			_, err := consumption.ClosePeriod(h.ctx, tx, h.tenantID, breakfast, h.adminPerson,
				"2026-09-01", "2026-09-30")
			return err
		})
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("the close finished while another transaction held the subject's lock (err=%v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	releaseLock()
	if err := <-holderDone; err != nil {
		t.Fatalf("lock holder: %v", err)
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the close never finished after the lock was released")
	}
}

// The claim: ADR-103's gapless numbering. A rolled-back issue returns its
// number — which a sequence, being non-transactional, could not do.
func TestFinance_IssueReturnsItsNumberOnRollback(t *testing.T) {
	h := newHarness(t, 0x14)
	hs := h.newHostel()
	breakfast := hs.addType(map[string]any{"name": "Breakfast"})
	alice := hs.admit(1, "alice")
	h.dispatch()
	h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/options", map[string]any{
		"code": "EGG", "name": "Egg", "charge_kind": "USAGE_FEE",
		"amount": "15.00", "currency": "INR"}, http.StatusCreated)
	h.must(alice.tok, "POST", "/consumption-types/"+breakfast+"/records",
		map[string]any{"occurred_on": "2026-09-05", "option_codes": []string{"EGG"}},
		http.StatusCreated, "Idempotency-Key", "k1")
	closed := h.must(h.adminTok, "POST", "/consumption-types/"+breakfast+"/close",
		map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, http.StatusCreated, "Idempotency-Key", "c1")
	invoice := str(closed["closed"].([]any)[0].(map[string]any), "invoice_id")

	errAbandon := errors.New("abandon the transaction")
	err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
		inv, err := finance.Issue(h.ctx, tx, h.tenantID, invoice, nil)
		if err != nil {
			return err
		}
		if numberOf(t, map[string]any{"number": *inv.Number}) != 1 {
			t.Errorf("the first number allocated was %s, want ...000001", *inv.Number)
		}
		return errAbandon
	})
	if !errors.Is(err, errAbandon) {
		t.Fatalf("issue: %v", err)
	}

	issued := h.must(h.adminTok, "POST", "/invoices/"+invoice+"/issue", nil,
		http.StatusOK, "Idempotency-Key", "i1")
	if got := numberOf(t, issued); got != 1 {
		t.Fatalf("after a rolled-back issue the invoice is number %d, want 1: "+
			"the abandoned number was not returned, so the series has a gap", got)
	}
}

func TestFinance_AnEmptyInvoiceCannotBeIssued(t *testing.T) {
	h := newHarness(t, 0x15)
	h.seed(`INSERT INTO party (id, kind, organization_id) VALUES
	        ('e0000000-0000-0000-0003-000000000015','ORGANIZATION',$1)`, h.tenantID)
	h.seed(`INSERT INTO party (id, kind, person_id) VALUES
	        ('e0000000-0000-0000-0004-000000000015','PERSON',$1)`, h.adminPerson)
	var invoice string
	if err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(h.ctx, `
			INSERT INTO invoice (id, tenant_id, payer_party_id, payee_party_id, currency, state)
			VALUES (gen_random_uuid(), $1,
			        'e0000000-0000-0000-0004-000000000015',
			        'e0000000-0000-0000-0003-000000000015', 'INR', 'DRAFT')
			RETURNING id::text`, h.tenantID).Scan(&invoice)
	}); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	h.refused(h.adminTok, "POST", "/invoices/"+invoice+"/issue", nil,
		http.StatusConflict, "invoice_empty", "Idempotency-Key", "e1")
	if n := h.count(`SELECT count(*) FROM invoice_number_series WHERE tenant_id = $1`, h.tenantID); n != 0 {
		t.Errorf("an empty invoice created %d number series rows; nothing should have been drawn", n)
	}
}

// /me reports six candidates (A3.9). finance_manager is the sixth, and no
// other test holds it.
func TestMe_ReportsFinanceManager(t *testing.T) {
	h := newHarness(t, 0x16)
	hs := h.newHostel()
	alice := hs.admit(1, "alice")
	h.dispatch()

	has := func(tok string) bool {
		out := h.must(tok, "GET", "/me", nil, http.StatusOK)
		for _, p := range out["permissions"].([]any) {
			if p == "tenant:finance_manager" {
				return true
			}
		}
		return false
	}
	if !has(h.adminTok) {
		t.Error("the tenant admin is not reported as finance_manager")
	}
	if has(alice.tok) {
		t.Error("a plain member is reported as finance_manager")
	}
}
