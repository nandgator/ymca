//go:build integration && dev

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// The claim (11.2's C1): the two stores are compared, in both directions. A
// grant that never arrived is found and re-queued; coverage that should not
// exist — for a membership no longer ACTIVE, or on a plan it has left — is
// found and revoked; and a grant still waiting in the outbox is nobody's drift.
func TestReconcile_CoverageBetweenPostgresAndTheGraph(t *testing.T) {
	h := newHarness(t, 0x27)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner"})
	other, _ := hs.planWith("resident-2", "Lunch")
	alice := hs.admit(1, "alice")
	bob := hs.admit(2, "bob")
	h.dispatch()

	run := func(repair bool) []membership.Drift {
		t.Helper()
		var found []membership.Drift
		if err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			var err error
			found, err = membership.ReconcileTenant(h.ctx, tx, h.tenantID, h.writer, h.writer, repair)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return found
	}
	covered := func(m member, plan string) outbox.Tuple {
		return outbox.Tuple{User: "membership:" + m.membership + "#covered", Relation: "covered_member",
			Object: "membership_plan:" + plan}
	}

	t.Run("a membership admitted and dispatched agrees with the graph", func(t *testing.T) {
		if got := run(false); len(got) != 0 {
			t.Fatalf("a clean tenant reports drift: %+v", got)
		}
	})

	t.Run("a grant still pending is not drift", func(t *testing.T) {
		carol := hs.admit(3, "carol") // admitted, not dispatched
		if got := run(false); len(got) != 0 {
			t.Fatalf("a grant waiting in the outbox was reported: %+v", got)
		}
		h.dispatch()
		_ = carol
	})

	t.Run("a missing grant is found, and only repaired when asked", func(t *testing.T) {
		if err := h.writer.DeleteTuples(h.ctx, []outbox.Tuple{covered(bob, hs.planID)}); err != nil {
			t.Fatal(err)
		}
		if h.graphAllowsRecording(bob.person, dinner) {
			t.Fatal("precondition: bob is no longer covered")
		}
		found := run(false)
		if len(found) != 1 || found[0].Kind != "MISSING" || found[0].MembershipID != bob.membership ||
			found[0].Tuple != covered(bob, hs.planID) || found[0].Repaired {
			t.Fatalf("report-only run found %+v, want one unrepaired MISSING covered tuple for bob", found)
		}
		if h.graphAllowsRecording(bob.person, dinner) {
			t.Fatal("a report-only run changed the graph")
		}
		if n := h.count(`SELECT count(*) FROM authorization_outbox
			WHERE dispatched_at IS NULL AND voided_at IS NULL AND fence_subject = $1`,
			"membership:"+bob.membership+"#covered"); n != 0 {
			t.Fatalf("a report-only run queued %d facts", n)
		}

		repaired := run(true)
		if len(repaired) != 1 || !repaired[0].Repaired {
			t.Fatalf("repair run found %+v", repaired)
		}
		if again := run(false); len(again) != 0 {
			t.Errorf("the repair is queued, so it is in flight and not drift: %+v", again)
		}
		h.dispatch()
		if !h.graphAllowsRecording(bob.person, dinner) {
			t.Error("after repair and dispatch bob is still not covered")
		}
		if got := run(false); len(got) != 0 {
			t.Errorf("still drift after repair: %+v", got)
		}
	})

	t.Run("a missing tenant edge or holder is found as well as a missing coverage", func(t *testing.T) {
		for _, lost := range []outbox.Tuple{
			{User: "tenant:" + h.tenantID, Relation: "tenant", Object: "membership:" + bob.membership},
			{User: "person:" + bob.person, Relation: "holder", Object: "membership:" + bob.membership},
		} {
			if err := h.writer.DeleteTuples(h.ctx, []outbox.Tuple{lost}); err != nil {
				t.Fatal(err)
			}
			found := run(true)
			if len(found) != 1 || found[0].Kind != "MISSING" || found[0].Tuple != lost || !found[0].Repaired {
				t.Fatalf("losing %+v: found %+v", lost, found)
			}
			h.dispatch()
			if ok, _ := h.writer.Has(h.ctx, lost); !ok {
				t.Errorf("%+v was not restored", lost)
			}
			if got := run(false); len(got) != 0 {
				t.Errorf("drift left after restoring %+v: %+v", lost, got)
			}
		}
	})

	t.Run("coverage of a suspended membership is found and revoked", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/memberships/"+alice.membership+"/suspend",
			map[string]any{"reason": "ARREARS"}, http.StatusOK, "Idempotency-Key", hs.key())
		if err := h.writer.WriteTuples(h.ctx, []outbox.Tuple{covered(alice, hs.planID)}); err != nil {
			t.Fatal(err)
		}
		if !h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("precondition: the stray tuple covers a suspended member")
		}
		found := run(false)
		if len(found) != 1 || found[0].Kind != "UNEXPECTED" || found[0].MembershipID != alice.membership {
			t.Fatalf("found %+v, want one UNEXPECTED for alice", found)
		}
		if !h.graphAllowsRecording(alice.person, dinner) {
			t.Fatal("a report-only run revoked")
		}
		if fixed := run(true); len(fixed) != 1 || !fixed[0].Repaired {
			t.Fatalf("repair run found %+v", fixed)
		}
		if h.graphAllowsRecording(alice.person, dinner) {
			t.Error("after repair a suspended member is still covered")
		}
		if got := run(false); len(got) != 0 {
			t.Errorf("still drift after repair: %+v", got)
		}
	})

	t.Run("coverage on a plan the member has left is found and revoked", func(t *testing.T) {
		// Bob is ACTIVE on the first plan; a tuple covering him on the other one
		// is what a move that half-failed would leave.
		if err := h.writer.WriteTuples(h.ctx, []outbox.Tuple{covered(bob, other)}); err != nil {
			t.Fatal(err)
		}
		found := run(true)
		if len(found) != 1 || found[0].Kind != "UNEXPECTED" || found[0].Tuple != covered(bob, other) || !found[0].Repaired {
			t.Fatalf("found %+v, want the stray other-plan tuple revoked", found)
		}
		if ok, _ := h.writer.Has(h.ctx, covered(bob, other)); ok {
			t.Error("the stray tuple survived repair")
		}
		if !h.graphAllowsRecording(bob.person, dinner) {
			t.Error("repair removed the coverage bob is owed")
		}
	})

	t.Run("a failed revocation is reported, not swallowed", func(t *testing.T) {
		if err := h.writer.WriteTuples(h.ctx, []outbox.Tuple{covered(alice, hs.planID)}); err != nil {
			t.Fatal(err)
		}
		err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			_, err := membership.ReconcileTenant(h.ctx, tx, h.tenantID, h.writer, failingRevoker{}, true)
			return err
		})
		if err == nil {
			t.Fatal("a repair that could not delete reported success")
		}
		h.must(h.adminTok, "GET", "/me", nil, http.StatusOK) // the server is unaffected
		run(true)
	})

	t.Run("the command prints a line per disagreement and counts what is left", func(t *testing.T) {
		if err := h.writer.WriteTuples(h.ctx, []outbox.Tuple{covered(alice, hs.planID)}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		unresolved, _, err := reconcileAll(h.ctx, h.pool, h.writer, false, &out)
		if err != nil {
			t.Fatal(err)
		}
		var mine int
		for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			var d struct {
				TenantID     string `json:"tenant_id"`
				MembershipID string `json:"membership_id"`
				Kind         string `json:"kind"`
			}
			if json.Unmarshal(line, &d) == nil && d.TenantID == h.tenantID {
				mine++
				if d.Kind != "UNEXPECTED" || d.MembershipID != alice.membership {
					t.Errorf("line %s", line)
				}
			}
		}
		if mine != 1 || unresolved < 1 {
			t.Errorf("%d lines for this tenant, %d unresolved: want 1 and at least 1", mine, unresolved)
		}
		out.Reset()
		if _, _, err := reconcileAll(h.ctx, h.pool, h.writer, true, &out); err != nil {
			t.Fatal(err)
		}
		if got := run(false); len(got) != 0 {
			t.Errorf("drift left after the command repaired: %+v", got)
		}
	})
}
