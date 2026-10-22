// finance_handlers.go is A3.11: rates, invoices, issue and payments.
//
// Invoices are not created by an endpoint. They begin in handleClosePeriod,
// which is the only publisher Finance has; see consumption_handlers.go.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/finance"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/page"
)

// financeWrite is the relation rates, issue and payments check, on the tenant.
// finance_reader would let a bursar's auditor issue invoices.
const financeWrite = "finance_manager"

func handleCreateRate(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, financeWrite) {
			return
		}
		var body struct {
			Amount   string  `json:"amount"`
			Currency string  `json:"currency"`
			From     string  `json:"from"`
			To       *string `json:"to"`
		}
		if !readBody(w, r, &body) {
			return
		}
		tenantID, typeID := r.PathValue("tenant"), r.PathValue("type")
		var created finance.Rate
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			created, err = finance.CreateRate(r.Context(), tx, tenantID, typeID, finance.NewRate{
				Amount: body.Amount, Currency: body.Currency, From: body.From, To: body.To,
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

func handleListInvoices(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, "finance_reader") {
			return
		}
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		tenantID := r.PathValue("tenant")
		var rows []finance.Invoice
		err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			rows, err = finance.List(r.Context(), tx, tenantID, params)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, finance.InvoiceKey))
	}
}

func invoiceObject(r *http.Request, id string) authz.Object {
	return authz.Object{Type: "invoice", ID: id, TenantID: r.PathValue("tenant")}
}

// handleGetInvoice checks `viewer` as the caller's person and principal
// together — the payer tuple is a person's, `finance_reader from tenant` a
// principal's — and a DRAFT additionally needs finance_reader: a payer is
// not shown a bill nobody has issued. Anyone else gets 404 (A3.3).
func handleGetInvoice(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("invoice")
		person, ok := callerPerson(w, r)
		if !ok {
			return
		}
		allowed, proceed := checkObject(w, r, pool, fga, logger, invoiceObject(r, id), "viewer", person)
		if !proceed {
			return
		}
		if !allowed {
			httpx.WriteError(w, httpx.CodeForbidden, "not permitted")
			return
		}

		tenantID := r.PathValue("tenant")
		var inv finance.Invoice
		err := pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
			var err error
			inv, err = finance.Get(r.Context(), tx, tenantID, id)
			return err
		})
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		if inv.State == "DRAFT" {
			reader, proceed := checkObject(w, r, pool, fga, logger, authz.Object{
				Type: "tenant", ID: tenantID, TenantID: tenantID,
			}, "finance_reader", "")
			if !proceed {
				return
			}
			if !reader {
				httpx.WriteError(w, httpx.CodeNotFound, "not found")
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, inv)
	}
}

func handleIssueInvoice(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeTenant(w, r, pool, fga, logger, financeWrite) {
			return
		}
		id := r.PathValue("invoice")
		var body struct {
			DueAt *string `json:"due_at"`
		}
		raw, err := httpx.ReadOptionalJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
			return
		}
		var due *time.Time
		if body.DueAt != nil {
			t, err := time.Parse(time.RFC3339, *body.DueAt)
			if err != nil {
				httpx.WriteError(w, httpx.CodeInvalidRequest, "due_at is not an RFC 3339 timestamp")
				return
			}
			due = &t
		}
		key, err := httpx.IdempotencyKey(r,
			fmt.Sprintf("POST /t/{t}/invoices/%s/issue", id), raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		tenantID := r.PathValue("tenant")
		runIdempotent(w, r, pool, logger, key, http.StatusOK,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				return finance.Issue(ctx, tx, tenantID, id, due)
			})
	}
}

func handlePayInvoice(pool *db.DB, fga *authz.FGA, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := callerPerson(w, r)
		if !ok || !authorizeTenant(w, r, pool, fga, logger, financeWrite) {
			return
		}
		id := r.PathValue("invoice")
		var body struct {
			Method            string  `json:"method"`
			Amount            string  `json:"amount"`
			Currency          string  `json:"currency"`
			ProviderReference *string `json:"provider_reference"`
			ReceivedAt        *string `json:"received_at"`
		}
		raw, err := httpx.ReadJSON(r, &body)
		if err != nil {
			httpx.WriteError(w, httpx.CodeInvalidRequest, "request body could not be read as JSON")
			return
		}
		var received *time.Time
		if body.ReceivedAt != nil {
			t, err := time.Parse(time.RFC3339, *body.ReceivedAt)
			if err != nil {
				httpx.WriteError(w, httpx.CodeInvalidRequest, "received_at is not an RFC 3339 timestamp")
				return
			}
			received = &t
		}
		key, err := httpx.IdempotencyKey(r,
			fmt.Sprintf("POST /t/{t}/invoices/%s/payments", id), raw)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		tenantID := r.PathValue("tenant")
		runIdempotent(w, r, pool, logger, key, http.StatusCreated,
			func(ctx context.Context, tx pgx.Tx) (any, error) {
				return finance.RecordPayment(ctx, tx, tenantID, id, finance.NewPayment{
					Method: body.Method, Amount: body.Amount, Currency: body.Currency,
					ProviderReference: body.ProviderReference, ReceivedAt: received,
					RecordedBy: actor,
				})
			})
	}
}

// handleMeInvoices lists the invoices billed to the caller, drafts excluded.
// Like /me/consumption it is scoped by identity and runs no graph check.
func handleMeInvoices(pool *db.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, err := httpx.PageParams(r)
		if err != nil {
			writeDomainError(w, r, logger, err)
			return
		}
		principal, _ := httpx.PrincipalFromContext(r.Context())
		tenantID := r.PathValue("tenant")
		var rows []finance.Invoice
		if principal.PersonID != "" {
			err = pool.InTenantTx(r.Context(), tenantID, func(tx pgx.Tx) error {
				var err error
				rows, err = finance.ListOwn(r.Context(), tx, tenantID, principal.PersonID, params)
				return err
			})
			if err != nil {
				writeDomainError(w, r, logger, err)
				return
			}
		}
		httpx.WriteJSON(w, http.StatusOK, page.Of(rows, params, finance.InvoiceKey))
	}
}
