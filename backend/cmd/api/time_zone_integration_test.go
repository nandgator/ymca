//go:build integration && dev

package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/consumption"
)

// ADR-119: "today" is the tenant's. Two zones are run because at any instant at
// least one of them is on a different calendar day from UTC — Kiritimati is
// UTC+14 and Pago Pago UTC-11, and no hour has both on the UTC date — so a site
// that went back to UTC fails for one of the two whatever time the suite runs.
func TestTenantTimeZone_TodayIsTheTenants(t *testing.T) {
	for i, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		t.Run(zone, func(t *testing.T) { runTimeZoneCases(t, 0x20+i, zone) })
	}
}

func runTimeZoneCases(t *testing.T, n int, zone string) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	day := func(offset int) string { return time.Now().In(loc).AddDate(0, 0, offset).Format("2006-01-02") }
	startedOn := day(0)

	h := newHarness(t, n)
	h.seed(`UPDATE tenant SET time_zone = $2 WHERE id = $1`, h.tenantID, zone)
	hs := h.newHostel()
	dinner := hs.addType(map[string]any{"name": "Dinner", "obligates": true,
		"recurrence": "DAILY", "record_mode": "EITHER"})
	alice := hs.admit(1, "alice")
	h.dispatch()

	t.Run("/me tells the client the zone, the day and the offset", func(t *testing.T) {
		out := h.must(alice.tok, "GET", "/me", nil, http.StatusOK)
		_, wantOffset := time.Now().In(loc).Zone()
		if str(out, "time_zone") != zone {
			t.Errorf("time_zone = %v, want %s", out["time_zone"], zone)
		}
		if got, _ := out["utc_offset_seconds"].(float64); int(got) != wantOffset {
			t.Errorf("utc_offset_seconds = %v, want %d", out["utc_offset_seconds"], wantOffset)
		}
		if str(out, "today") != day(0) && str(out, "today") != day(1) { // tolerate crossing midnight
			t.Errorf("today = %v, want %s", out["today"], day(0))
		}
	})

	rec := func(d string) (int, map[string]any) {
		return h.call(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": d}, "Idempotency-Key", hs.key())
	}
	t.Run("the future begins after the tenant's today, not after UTC's", func(t *testing.T) {
		if status, out := rec(day(0)); status != http.StatusCreated {
			t.Fatalf("recording the tenant's own today (%s) = %d %v", day(0), status, out)
		}
		status, out := rec(day(1))
		assertCode(t, status, out, http.StatusBadRequest, "invalid_request")
	})

	// A window is measured from the instant the tenant's day began. The window
	// is set to land one hour either side of now, which the two readings of
	// "the day began" (the tenant's midnight and UTC's) disagree about in one
	// direction or the other in both zones.
	record := rec
	var recordID string
	t.Run("a correction is late from the tenant's midnight", func(t *testing.T) {
		status, out := record(day(-1))
		if status != http.StatusCreated {
			t.Fatalf("seed record = %d %v", status, out)
		}
		recordID = str(out, "id")
		for _, c := range []struct {
			shift string
			late  bool
		}{{"- interval '1 hour'", true}, {"+ interval '1 hour'", false}} {
			if err := h.tx(func(q func(string, ...any)) {
				q(fmt.Sprintf(`UPDATE consumption_type SET correction_window =
					now() - tenant_day_start(tenant_id, $2::date) %s WHERE id = $1`, c.shift), dinner, day(-1))
			}); err != nil {
				t.Fatal(err)
			}
			var head consumption.Head
			if err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
				var err error
				head, err = consumption.PeekHead(h.ctx, tx, h.tenantID, recordID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if head.Late != c.late {
				t.Errorf("window ending %s now: Late = %v, want %v", c.shift, head.Late, c.late)
			}
			// Correct recomputes lateness under lock; asking the same question
			// through it, so the two cannot shadow each other.
			err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
				_, err := consumption.Correct(h.ctx, tx, h.tenantID, recordID,
					consumption.Correction{Void: true},
					consumption.Actor{PersonID: alice.person, PrincipalID: alice.princ, RequestID: "tz"}, false)
				return err
			})
			if gotLate := errors.Is(err, consumption.ErrWindowClosed); gotLate != c.late || (!c.late && err != nil) {
				t.Errorf("window ending %s now: Correct returned %v, want window-closed=%v", c.shift, err, c.late)
			}
		}
	})

	// The term check runs on the person route (ADR-118). A term of exactly the
	// tenant's own day is current; read as UTC it is not, whenever the dates differ.
	t.Run("a membership whose term is the tenant's today is current", func(t *testing.T) {
		if err := h.tx(func(q func(string, ...any)) {
			q(`UPDATE membership SET valid_from = $2::date, valid_until = $2::date WHERE id = $1::uuid`,
				alice.membership, day(0))
		}); err != nil {
			t.Fatal(err)
		}
		status, out := h.call(alice.tok, "POST", "/consumption-types/"+dinner+"/records",
			map[string]any{"occurred_on": day(-2)}, "Idempotency-Key", hs.key())
		if status != http.StatusCreated {
			t.Fatalf("a member within a term of %s was refused: %d %v", day(0), status, out)
		}
	})

	t.Run("a terminated membership is owed through the tenant's today", func(t *testing.T) {
		h.must(h.adminTok, "POST", "/consumption-types/"+dinner+"/obligations", map[string]any{
			"subject_person_id": alice.person, "membership_id": alice.membership,
			"effective_from": day(-30),
		}, http.StatusCreated)
		if status, out := h.transition(h.adminTok, alice.membership, "terminate", map[string]any{}, hs.key()); status != http.StatusOK {
			t.Fatalf("terminate = %d %v", status, out)
		}
		var upper string
		if err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(h.ctx, `SELECT to_char(upper(effective) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI')
				FROM consumption_obligation WHERE tenant_id = $1 AND source_id = $2::uuid`,
				h.tenantID, alice.membership).Scan(&upper)
		}); err != nil {
			t.Fatal(err)
		}
		if want := day(1) + "T00:00"; upper != want {
			t.Errorf("the obligation ends %s, want %s", upper, want)
		}
	})

	if day(0) != startedOn {
		t.Skipf("the tenant's day changed from %s to %s during the run", startedOn, day(0))
	}
}

// The invalid zone, and the three functions, at the database: nothing here
// needs a server.
func TestTenantTimeZone_TheDatabaseRefusesWhatIsNotAZone(t *testing.T) {
	h := newHarness(t, 0x22)
	for _, bad := range []string{"EST", "Mars/Olympus_Mons", "", "UTC+5", "posix/Asia/Kolkata"} {
		if err := h.tx(func(q func(string, ...any)) {
			q(`UPDATE tenant SET time_zone = $2 WHERE id = $1`, h.tenantID, bad)
		}); err == nil {
			t.Errorf("time_zone %q was accepted", bad)
		}
	}
	if n := h.count(`SELECT count(*) FROM tenant WHERE id = $1 AND time_zone = 'UTC'`, h.tenantID); n != 1 {
		t.Errorf("a refused update changed the zone")
	}

	var fy25, fy26 string
	if err := h.pool.Pool().QueryRow(h.ctx,
		`SELECT financial_year('2026-03-31'), financial_year('2026-04-01')`).Scan(&fy25, &fy26); err != nil {
		t.Fatal(err)
	}
	if fy25 != "2025-26" || fy26 != "2026-27" {
		t.Errorf("financial_year: 31 March = %s, 1 April = %s; want 2025-26, 2026-27", fy25, fy26)
	}

	var start, none *string
	if err := h.pool.InTenantTx(h.ctx, h.tenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(h.ctx, `UPDATE tenant SET time_zone = 'Asia/Kolkata' WHERE id = $1`, h.tenantID); err != nil {
			return err
		}
		if err := tx.QueryRow(h.ctx, `SELECT to_char(tenant_day_start($1, '2026-03-01') AT TIME ZONE 'UTC',
			'YYYY-MM-DD"T"HH24:MI')`, h.tenantID).Scan(&start); err != nil {
			return err
		}
		return tx.QueryRow(h.ctx, `SELECT tenant_today('00000000-0000-0000-0000-000000000000')::text`).Scan(&none)
	}); err != nil {
		t.Fatal(err)
	}
	if start == nil || *start != "2026-02-28T18:30" {
		t.Errorf("a Kolkata day began at %v, want 2026-02-28T18:30 UTC", start)
	}
	if none != nil {
		t.Errorf("tenant_today of a tenant that does not exist = %q, want NULL", *none)
	}
}
