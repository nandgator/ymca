//go:build integration && dev

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nandgator/ymca/backend/internal/auth"
	authdev "github.com/nandgator/ymca/backend/internal/auth/dev"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/outbox"
	"io"
	"log/slog"
)

// The claim: a deployment with nothing in it can get its first platform
// operator, and that operator can provision a tenant whose owner then holds
// admin — with no row or tuple written by hand. Before this command the
// operator's authority was a line in a test fixture.
func TestBootstrap_FromNothingToAProvisionedTenant(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{
		DatabaseURL:   mustEnvVar(t, "YMCA_DATABASE_URL"),
		FGAAPIURL:     mustEnvVar(t, "YMCA_FGA_API_URL"),
		FGAStoreID:    mustEnvVar(t, "YMCA_FGA_STORE_ID"),
		FGAModelID:    mustEnvVar(t, "YMCA_FGA_MODEL_ID"),
		DevAuthSecret: mustEnvVar(t, "YMCA_DEV_AUTH_SECRET"),
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fga, err := authz.NewFGA(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const opSubject, ownerSubject = "bootstrap-test-operator", "bootstrap-test-owner"
	var tenantID, opPrincipal string
	cleanup := func() {
		bg := context.Background()
		if opPrincipal != "" {
			_ = writer.DeleteTuples(bg, []outbox.Tuple{{
				User: "principal:" + opPrincipal, Relation: "operator", Object: "platform:main"}})
		}
		if tenantID != "" {
			var agg []string
			rows, _ := pool.Pool().Query(bg, `SELECT aggregate_id::text FROM authorization_outbox
				WHERE payload->>'tenant_id' = $1 OR aggregate_id = $1::uuid`, tenantID)
			if rows != nil {
				for rows.Next() {
					var id string
					if rows.Scan(&id) == nil {
						agg = append(agg, id)
					}
				}
				rows.Close()
			}
			var tuples []outbox.Tuple
			trows, _ := pool.Pool().Query(bg, `SELECT fence_subject, fence_relation, fence_object
				FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])
				AND dispatched_at IS NOT NULL AND fence_subject IS NOT NULL`, agg)
			if trows != nil {
				for trows.Next() {
					var tp outbox.Tuple
					if trows.Scan(&tp.User, &tp.Relation, &tp.Object) == nil {
						tuples = append(tuples, tp)
					}
				}
				trows.Close()
			}
			_ = writer.DeleteTuples(bg, tuples)
			_, _ = pool.Pool().Exec(bg, `DELETE FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])`, agg)
			_, _ = pool.Pool().Exec(bg, `DELETE FROM tenant WHERE id = $1::uuid`, tenantID)
		}
		_, _ = pool.Pool().Exec(bg, `DELETE FROM principal WHERE idp_subject = ANY($1)`,
			[]string{opSubject, ownerSubject})
		_, _ = pool.Pool().Exec(bg, `DELETE FROM person WHERE display_name = ANY($1)`,
			[]string{"Bootstrap Operator", "Bootstrap Owner"})
	}
	cleanup()
	t.Cleanup(cleanup)

	id1, created1, err := bootstrapOperator(ctx, pool, writer, opSubject, "Bootstrap Operator")
	if err != nil || !created1 {
		t.Fatalf("first bootstrap: id %q created %v err %v", id1, created1, err)
	}
	opPrincipal = id1
	id2, created2, err := bootstrapOperator(ctx, pool, writer, opSubject, "Bootstrap Operator")
	if err != nil || created2 || id2 != id1 {
		t.Fatalf("second bootstrap: id %q created %v err %v; want the same operator, not a second", id2, created2, err)
	}
	var n int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM principal WHERE idp_subject = $1`, opSubject).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d principals for the subject (err %v), want 1", n, err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authenticator, err := auth.Open("dev", auth.Deps{DB: pool, DevAuthSecret: cfg.DevAuthSecret})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerRoutes(mux, pool, fga, writer, logger)
	srv := httptest.NewServer(httpx.Chain(mux, httpx.Recover(logger), httpx.RequestID(),
		httpx.Logging(logger), httpx.Authenticate(authenticator, logger)))
	defer srv.Close()

	opTok, err := authdev.MintPlatform([]byte(cfg.DevAuthSecret), opSubject,
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"legal_name": "Bootstrap Assoc", "display_name": "Bootstrap Assoc", "jurisdiction": "IN",
		"owner": map[string]any{"display_name": "Bootstrap Owner", "idp_subject": ownerSubject},
	})
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/platform/tenants", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+opTok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("the bootstrapped operator could not provision a tenant: %d %v", resp.StatusCode, created)
	}
	tenantID, _ = created["id"].(string)
	if tenantID == "" {
		tenantID, _ = created["tenant_id"].(string)
	}
	if tenantID == "" {
		t.Fatalf("no tenant id in %v", created)
	}

	disp := outbox.New(pool.Pool(), writer, outboxRenderers(), logger)
	for i := 0; i < 5; i++ {
		if n, err := disp.Once(ctx); err != nil || n == 0 {
			break
		}
	}
	ownerTok, err := authdev.Mint([]byte(cfg.DevAuthSecret), ownerSubject, tenantID,
		time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/v1/t/"+tenantID+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	perms, _ := me["permissions"].([]any)
	got := map[string]bool{}
	for _, p := range perms {
		got[p.(string)] = true
	}
	for _, want := range []string{"tenant:admin", "tenant:finance_manager", "tenant:may_register_person", "tenant:may_approve_membership"} {
		if !got[want] {
			t.Errorf("the new owner lacks %s; permissions %v", want, perms)
		}
	}
}
