// Command api serves the tenant-plane and platform-plane HTTP API.
//
// main.go is wiring only: it constructs the pool, the OpenFGA client and
// the auth provider once at start-up, builds the middleware chain
// (internal/httpx) and shuts everything down cleanly on SIGINT/SIGTERM.
// Authorization decisions live in internal/authz, authentication in
// internal/auth — this file does not make either kind of decision itself.
//
//	api                              serve the API (default, no args)
//	api mint-token <sub> <tenant>    //go:build dev only — see devtoken.go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nandgator/ymca/backend/internal/auth"
	"github.com/nandgator/ymca/backend/internal/authz"
	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/httpx"
	"github.com/nandgator/ymca/backend/internal/membership"
	"github.com/nandgator/ymca/backend/internal/organization"
	"github.com/nandgator/ymca/backend/internal/outbox"
)

// commands holds CLI subcommands beyond "serve the API", registered by
// //go:build dev files from their own init(). Empty in a deployment
// build — there is nothing to register into it, so `api mint-token ...`
// reports an unknown command instead of failing to compile or, worse,
// silently minting a credential in a build that was never supposed to be
// able to (D2).
var commands = map[string]func(args []string) error{}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api: fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if len(os.Args) > 1 {
		cmd, ok := commands[os.Args[1]]
		if !ok {
			return fmt.Errorf("unknown command %q", os.Args[1])
		}
		return cmd(os.Args[2:])
	}
	return serve(logger)
}

func serve(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	if err := pool.Health(ctx); err != nil {
		return fmt.Errorf("database health check: %w", err)
	}

	fga, err := authz.NewFGA(cfg)
	if err != nil {
		return fmt.Errorf("open openfga client: %w", err)
	}

	// D2's runtime gate: even a binary compiled with -tags dev refuses to
	// start unless the operator explicitly names "dev" — there is no
	// default that could select it by accident.
	authenticator, err := auth.Open(cfg.AuthProvider, auth.Deps{
		DB:            pool,
		DevAuthSecret: cfg.DevAuthSecret,
	})
	if err != nil {
		return fmt.Errorf("open auth provider %q: %w", cfg.AuthProvider, err)
	}

	// The outbox dispatcher (ADR-101, 8.3). It runs in-process because there
	// is one deployment unit today; SKIP LOCKED means several replicas drain
	// the table safely, so extracting it to its own binary is a deployment
	// decision rather than a correctness one.
	//
	// An unknown event_type fails its row loudly and leaves it pending for
	// 7.4's staleness alert, rather than being skipped.
	fgaWriter, err := outbox.NewFGAWriter(cfg)
	if err != nil {
		return fmt.Errorf("open outbox writer: %w", err)
	}
	dispatcher := outbox.New(pool.Pool(), fgaWriter, outboxRenderers(), logger)
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		dispatcher.Run(ctx)
	}()

	// TenantMatch is applied per route, not around the mux: ServeMux only
	// populates r.PathValue while resolving a specific route, so a
	// middleware wrapping the mux would see an empty tenant (ADR-105).
	mux := http.NewServeMux()
	tenantRoute := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, httpx.TenantMatch(pool, logger)(h))
	}

	// A3.7's platform plane. It sits behind PlatformOnly rather than
	// TenantMatch: there is no {tenant} to match, and a tenant-bound
	// credential must be refused here just as a platform credential is
	// refused on a tenant route (ADR-111). The two gates are exclusive.
	mux.Handle("POST /api/v1/platform/tenants",
		httpx.PlatformOnly(pool.Pool(), logger)(handleCreateTenant(pool, fga, logger)))

	tenantRoute("GET /api/v1/t/{tenant}/me", handleMe(pool, fga, logger))

	// A3.7 configuration. Without these the slice cannot run: admission
	// needs a plan, ADR-107's covered_member tuple needs one to point at,
	// and may_record resolves through `entitled` from a bundle.
	tenantRoute("POST /api/v1/t/{tenant}/entitlement-bundles",
		handleCreateBundle(pool, fga, logger))
	tenantRoute("GET /api/v1/t/{tenant}/entitlement-bundles",
		handleListBundles(pool, fga, logger))
	tenantRoute("POST /api/v1/t/{tenant}/entitlement-bundles/{bundle}/entitles",
		handleEntitle(pool, fga, logger))
	tenantRoute("POST /api/v1/t/{tenant}/membership-plans",
		handleCreatePlan(pool, fga, logger))
	tenantRoute("GET /api/v1/t/{tenant}/membership-plans",
		handleListPlans(pool, fga, logger))
	tenantRoute("POST /api/v1/t/{tenant}/consumption-types",
		handleCreateConsumptionType(pool, fga, logger))
	tenantRoute("GET /api/v1/t/{tenant}/consumption-types",
		handleListConsumptionTypes(pool, fga, logger))

	handler := httpx.Chain(mux,
		httpx.Recover(logger),
		httpx.RequestID(),
		httpx.Logging(logger),
		httpx.Authenticate(authenticator, logger),
	)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api: listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("api: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		// Run returns on ctx cancellation. Waiting for it means an in-flight
		// dispatch finishes its transaction rather than being cut off between
		// the OpenFGA write and the row's dispatched_at, which would leave
		// the row pending and redeliver a tuple that is already there —
		// harmless, but only because WriteTuples tolerates redelivery.
		<-dispatcherDone
		return err
	case err := <-errCh:
		return err
	}
}

// outboxRenderers maps an event type to the tuples the CURRENT model wants
// for it (ADR-101). The renderers live beside the code that publishes the
// facts, so adding an event without a renderer is one diff away from being
// noticed — and if it is not, the dispatcher fails that row loudly rather
// than skipping it.
func outboxRenderers() map[string]outbox.Renderer {
	all := make(map[string]outbox.Renderer)
	for _, set := range []map[string]outbox.Renderer{
		membership.Renderers(),
		organization.Renderers(),
	} {
		for event, render := range set {
			if _, dup := all[event]; dup {
				// Two packages claiming one event type would mean the
				// dispatcher renders whichever won the map iteration — a
				// silent, order-dependent choice between two meanings of
				// the same fact. Refuse at start-up instead.
				panic("outbox: two renderers registered for event " + event)
			}
			all[event] = render
		}
	}
	return all
}
