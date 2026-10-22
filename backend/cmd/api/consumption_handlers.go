// consumption_handlers.go is A3.10: options, obligations, records,
// corrections, absences and period close.
//
// The same four steps as every handler here — authorize, read, act, respond —
// with two things that are new to this file. Recording and correcting check
// permission on a consumption TYPE that the request names indirectly (a
// correction names a record, whose type is what the graph knows), and the
// subject of a record is a PERSON whose own entitlement is part of the
// decision, not only the caller's.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/consumption"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/finance"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/page"
)

func typeObject(r *http.Request, typeID string) authz.Object {
	return authz.Object{Type: "consumption_type", ID: typeID, TenantID: r.PathValue("tenant")}
}

// authorizeType checks a relation on the consumption type named in the path.
// TenantID is the request tenant for the reason authorizeUnit gives: the table
// is under RLS, so another tenant's type is unreadable and has no path to
// this principal in the graph.
func authorizeType(
	w http.ResponseWriter, r *http.Request,
	pool *db.DB, fga *authz.FGA, logger *slog.Logger, typeID, relation, alsoPerson string,
) bool {
	allowed, proceed := checkObject(w, r, pool, fga, logger,
		typeObject(r, typeID), relation, alsoPerson)
	if !proceed {
		return false
	}
	if !allowed {
		httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
		return false
	}
	return true
}

// callerPerson is the person the principal acts as. A principal with no
// person (a platform operator never reaches a tenant route, but a fixture may
// build one) can neither record nor be recorded as.
func callerPerson(w http.ResponseWriter, r *http.Request) (string, bool) {
	principal, ok := httpx.PrincipalFromContext(r.Context())
	if !ok || principal.PersonID == "" {
		httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
		return "", false
	}
	return principal.PersonID, true
}

func handleCreateOption(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		if !authorizeType(w, r, pool, fga, logger, typeID, "manager", "") {
			return
		}
		var body struct {
			Code       string `json:"code"`
			Name       string `json:"name"`
			ChargeKind string `json:"charge_kind"`
			Amount     string `json:"amount"`
			Currency   string `json:"currency"`
		}
		if !readBody(w, r, &body) {
			return
		}
		tenantID := r.PathValue("tenant")
		var created consumption.Option
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			created, err = consumption.CreateOption(r.Context(), tx, tenantID, typeID,
				consumption.NewOption{
					Code: body.Code, Name: body.Name, ChargeKind: body.ChargeKind,
					Amount: body.Amount, Currency: body.Currency,
				})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	}
}

// handleListOptions is checked as may_record, for the caller's person and
// principal: a member choosing what to add to a meal must be able to read the
// menu, and so must the staff who manage it (manager is inside may_record).
func handleListOptions(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		person, ok := callerPerson(w, r)
		if !ok || !authorizeType(w, r, pool, fga, logger, typeID, "may_record", person) {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		tenantID := r.PathValue("tenant")
		var rows []consumption.Option
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = consumption.ListOptions(r.Context(), tx, tenantID, typeID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, consumption.OptionKey))
	}
}

func handleCreateObligation(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		if !authorizeType(w, r, pool, fga, logger, typeID, "manager", "") {
			return
		}
		var body struct {
			SubjectPersonID string  `json:"subject_person_id"`
			MembershipID    string  `json:"membership_id"`
			EffectiveFrom   string  `json:"effective_from"`
			EffectiveUntil  *string `json:"effective_until"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.SubjectPersonID == "" || body.MembershipID == "" || body.EffectiveFrom == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest,
				"subject_person_id, membership_id and effective_from are required")
			return
		}
		tenantID := r.PathValue("tenant")
		var created consumption.Obligation
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			created, err = consumption.CreateObligation(r.Context(), tx, tenantID, typeID,
				consumption.NewObligation{
					SubjectPersonID: body.SubjectPersonID,
					MembershipID:    body.MembershipID,
					EffectiveFrom:   body.EffectiveFrom,
					EffectiveUntil:  body.EffectiveUntil,
				})
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	}
}

// handleRecord is A3.10's recording endpoint.
//
// The authorization is the one place in this API where two checks answer one
// question. Recording for oneself is may_record for the caller's person AND
// principal; recording for somebody else is may_record_for_other for the
// principal, plus the SUBJECT's own may_record — `person` is global (A2.1),
// so without the second a tenant's staff could bill any person in the
// instance.
func handleRecord(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		actor, ok := callerPerson(w, r)
		if !ok {
			return
		}

		var body struct {
			SubjectPersonID string   `json:"subject_person_id"`
			OccurredOn      string   `json:"occurred_on"`
			VariantCode     *string  `json:"variant_code"`
			OptionCodes     []string `json:"option_codes"`
		}
		raw, err := httpx.ReadJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
			return
		}
		if body.OccurredOn == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "occurred_on is required")
			return
		}
		subject := body.SubjectPersonID
		if subject == "" {
			subject = actor
		}

		if subject == actor {
			if !authorizeType(w, r, pool, fga, logger, typeID, "may_record", actor) {
				return
			}
		} else {
			if !authorizeType(w, r, pool, fga, logger, typeID, "may_record_for_other", "") {
				return
			}
			entitled, err := fga.Check(r.Context(), "person:"+subject, "may_record",
				"consumption_type:"+typeID, nil)
			if err != nil {
				logger.ErrorContext(r.Context(), "subject entitlement check failed",
					"error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
				httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
				return
			}
			if !entitled {
				httpx.WriteError(w, httpx.CodeForbidden,
					"that person is not entitled to this consumption type")
				return
			}
		}

		// The type id is part of the key's scope: the body names no type, so
		// the same key and body against two types would otherwise replay
		// the first type's record for the second.
		key, err := httpx.IdempotencyKey(r,
			fmt.Sprintf("POST /t/{t}/consumption-types/%s/records", typeID), raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		runIdempotent(w, r, pool, logger, key, http.StatusCreated,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				return consumption.RecordUse(ctx, tx, tenantID, consumption.NewRecord{
					TypeID:          typeID,
					SubjectPersonID: subject,
					ActorPersonID:   actor,
					OccurredOn:      body.OccurredOn,
					VariantCode:     body.VariantCode,
					OptionCodes:     body.OptionCodes,
				})
			})
	}
}

// handleCorrection supersedes a record (ADR-098).
//
// Two permissions, the second only when needed: may_correct always, and
// may_correct_late when the record is past its type's window. The window is
// read BEFORE the write to decide which to ask for, and recomputed under lock
// inside Correct, which refuses a late correction whose caller was not
// granted the second. A denial of the second is a 409 the caller can act on —
// it already holds may_correct — rather than the bare 403.
func handleCorrection(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recordID := r.PathValue("record")
		actor, ok := callerPerson(w, r)
		if !ok {
			return
		}
		principal, _ := httpx.PrincipalFromContext(r.Context())
		tenantID := r.PathValue("tenant")

		var body struct {
			Void        bool     `json:"void"`
			VariantCode *string  `json:"variant_code"`
			OptionCodes []string `json:"option_codes"`
		}
		raw, err := httpx.ReadJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
			return
		}

		// A3.10: an absent record is a 404 before there is a type to check
		// against. Record ids are unguessable, which is what makes that cheap.
		var head consumption.Head
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			head, err = consumption.PeekHead(r.Context(), tx, tenantID, recordID)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		if !authorizeType(w, r, pool, fga, logger, head.TypeID, "may_correct", "") {
			return
		}
		lateAllowed := false
		if head.Late {
			allowed, proceed := checkObject(w, r, pool, fga, logger,
				typeObject(r, head.TypeID), "may_correct_late", "")
			if !proceed {
				return
			}
			if !allowed {
				httpx.WriteError(w, httpx.CodeWindowClosed,
					consumption.ErrWindowClosed.Error())
				return
			}
			lateAllowed = true
		}

		key, err := httpx.IdempotencyKey(r,
			fmt.Sprintf("POST /t/{t}/consumption-records/%s/correction", recordID), raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		runIdempotent(w, r, pool, logger, key, http.StatusCreated,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				return consumption.Correct(ctx, tx, tenantID, recordID,
					consumption.Correction{
						Void: body.Void, VariantCode: body.VariantCode, OptionCodes: body.OptionCodes,
					},
					consumption.Actor{
						PersonID: actor, PrincipalID: principal.ID,
						RequestID: httpx.RequestIDFromContext(ctx),
					}, lateAllowed)
			})
	}
}

func handleListRecords(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		if !authorizeType(w, r, pool, fga, logger, typeID, "may_read", "") {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		q := r.URL.Query()
		filter := consumption.ListFilter{
			SubjectPersonID: q.Get("subject"),
			From:            q.Get("from"),
			To:              q.Get("to"),
			History:         q.Get("history") == "true",
		}
		tenantID := r.PathValue("tenant")
		var rows []consumption.Record
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = consumption.ListRecords(r.Context(), tx, tenantID, typeID, filter, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, consumption.RecordKey))
	}
}

// handleDeclareAbsence: self-declaration needs an ACTIVE membership, read from
// PostgreSQL — the one handler in the slice with no graph check first, and
// why is in A3.10. Declaring for another person needs tenant:admin.
func handleDeclareAbsence(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := callerPerson(w, r)
		if !ok {
			return
		}
		var body struct {
			SubjectPersonID string `json:"subject_person_id"`
			From            string `json:"from"`
			To              string `json:"to"`
		}
		if !readBody(w, r, &body) {
			return
		}
		if body.From == "" || body.To == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "from and to are required")
			return
		}
		subject := body.SubjectPersonID
		if subject == "" {
			subject = actor
		}
		tenantID := r.PathValue("tenant")

		if subject != actor {
			if !authorizeTenant(w, r, pool, fga, logger, "admin") {
				return
			}
		}

		var created consumption.Absence
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			if subject == actor {
				member, err := membership.IsActiveMember(r.Context(), tx, tenantID, actor)
				if err != nil {
					return err
				}
				if !member {
					return errNotAMember
				}
			}
			var err error
			created, err = consumption.DeclareAbsence(r.Context(), tx, tenantID,
				consumption.NewAbsence{
					SubjectPersonID: subject, DeclaredByPersonID: actor,
					From: body.From, To: body.To,
				})
			return err
		})
		if err == errNotAMember {
			httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
			return
		}
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	}
}

var errNotAMember = fmt.Errorf("api: caller holds no active membership")

// handleWithdrawAbsence: the declarer may withdraw without asking the graph;
// anyone else must be a tenant admin. The declarer path runs first so that
// the ordinary case writes no DENY to the audit trail.
func handleWithdrawAbsence(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		absenceID := r.PathValue("absence")
		actor, ok := callerPerson(w, r)
		if !ok {
			return
		}
		tenantID := r.PathValue("tenant")

		var declarer string
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			declarer, err = consumption.AbsenceDeclarer(r.Context(), tx, tenantID, absenceID)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		admin := false
		if declarer != actor {
			if !authorizeTenant(w, r, pool, fga, logger, "admin") {
				return
			}
			admin = true
		}

		var out consumption.Absence
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			out, err = consumption.WithdrawAbsence(r.Context(), tx, tenantID, absenceID, actor, admin)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}

// handleClosePeriod is the billing handoff (05.10.8).
//
// This function is the one place Consumption and Finance meet. Consumption
// publishes units and vocabulary; Finance composes. There is no event bus
// (B6), so the handoff is a direct call inside the closing transaction, and
// the two contexts do not import each other.
func handleClosePeriod(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		typeID := r.PathValue("type")
		actor, ok := callerPerson(w, r)
		if !ok {
			return
		}
		if !authorizeType(w, r, pool, fga, logger, typeID, "may_close_period", "") {
			return
		}
		var body struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		raw, err := httpx.ReadJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
			return
		}
		if body.From == "" || body.To == "" {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "from and to are required")
			return
		}
		key, err := httpx.IdempotencyKey(r,
			fmt.Sprintf("POST /t/{t}/consumption-types/%s/close", typeID), raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		tenantID := r.PathValue("tenant")
		runIdempotent(w, r, pool, logger, key, http.StatusCreated,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				closed, err := consumption.ClosePeriod(ctx, tx, tenantID, typeID, actor, body.From, body.To)
				if err != nil {
					return nil, err
				}
				for i := range closed {
					c := &closed[i]
					lines := make([]finance.OptionLine, 0, len(c.Summary.Options))
					for _, o := range c.Summary.Options {
						lines = append(lines, finance.OptionLine{
							Code: o.Code, Name: o.Name, ChargeKind: o.ChargeKind,
							Amount: o.Amount, Currency: o.Currency, Count: o.Count,
						})
					}
					c.InvoiceID, err = finance.ComposeConsumption(ctx, tx, tenantID,
						finance.ConsumptionClose{
							CloseID: c.ID, TypeID: c.TypeID, TypeName: c.TypeName,
							SubjectPersonID: c.SubjectPersonID, From: c.From, To: c.To,
							ObligatedUnits: c.ObligatedUnits, Options: lines,
						})
					if err != nil {
						return nil, err
					}
				}
				if closed == nil {
					closed = []consumption.Closed{}
				}
				return struct {
					Closed []consumption.Closed `json:"closed"`
				}{closed}, nil
			})
	}
}

// handleMeConsumption lists the caller's own records, newest first. The rows
// are the caller's by identity — their person id is the filter — and RLS
// bounds the tenant, so no graph check runs (A3.11).
func handleMeConsumption(pool *db.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		principal, _ := httpx.PrincipalFromContext(r.Context())
		tenantID := r.PathValue("tenant")
		var rows []consumption.Record
		if principal.PersonID != "" {
			err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
				var err error
				rows, err = consumption.ListOwn(r.Context(), tx, tenantID, principal.PersonID, params)
				return err
			})
			if err != nil {
				writeDomainError(w, r, logger, err)
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, consumption.RecordKey))
	}
}

// handleMeConsumptionTypes lists the active types the caller may record
// against — what a member's "record my meal" screen offers.
//
// GET /consumption-types is a tenant-admin list and a member cannot read it,
// yet a member must be told what they may record; "a permission on any other
// object is reported by the endpoint that returns that object" (A3.9). One
// graph question per row, never ListObjects (ADR-104 refuses it).
//
// The questions go to OpenFGA directly and not through authz.Check: this is
// VISIBILITY, not a decision. Nothing is granted by appearing here — recording
// runs the full 6.1 check — and a stranger listing the catalogue would
// otherwise write a DENY to the audit trail for every type in the tenant.
// The person is asked first and the principal second, for the reason
// authz.Request.AlsoAsPerson gives.
func handleMeConsumptionTypes(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		principal, _ := httpx.PrincipalFromContext(r.Context())
		tenantID := r.PathValue("tenant")

		var fetched []consumption.Type
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			fetched, err = consumption.ListActive(r.Context(), tx, tenantID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}

		// The cursor is taken from the fetched page, before filtering, so a
		// page the filter shortens still resumes where it stopped.
		pg := page.Of(fetched, params, consumption.TypeKey)
		visible := make([]consumption.Type, 0, len(pg.Items))
		for _, t := range pg.Items {
			object := "consumption_type:" + t.ID
			subjects := []string{"principal:" + principal.ID}
			if principal.PersonID != "" {
				subjects = []string{"person:" + principal.PersonID, "principal:" + principal.ID}
			}
			for _, subject := range subjects {
				ok, err := fga.Check(r.Context(), subject, "may_record", object, nil)
				if err != nil {
					logger.ErrorContext(r.Context(), "visibility check failed",
						"error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
					httpx.WriteError(w, httpx.CodeInternal, "authorization check failed")
					return
				}
				if ok {
					visible = append(visible, t)
					break
				}
			}
		}
		pg.Items = visible
		httpx.WriteJSON(w, http.StatusOK, pg)
	}
}
