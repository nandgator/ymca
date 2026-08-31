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

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/t/{tenant}/me",
		httpx.TenantMatch(pool, logger)(http.HandlerFunc(handleMe(pool, fga, logger))))

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
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
