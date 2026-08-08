// Command api is the YMCA mess-management backend. It wires every
// concrete adapter (Postgres repos, console OTP sender) to the app-layer
// services and serves them over HTTP. This file should stay wiring-only —
// if you find yourself adding an if-statement with business meaning here,
// it belongs in internal/app or internal/domain instead.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/auth"
	"github.com/ymca/mess-backend/internal/httpapi"
	"github.com/ymca/mess-backend/internal/storage/postgres"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	databaseURL := requireEnv("DATABASE_URL")
	port := getEnv("PORT", "8080")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Println("connected to postgres")

	hostels := postgres.HostelRepo{DB: pool}
	members := postgres.MemberRepo{DB: pool}
	secretaries := postgres.SecretaryRepo{DB: pool}
	centralAdmins := postgres.CentralAdminRepo{DB: pool}
	entries := postgres.EntryRepo{DB: pool}
	leaves := postgres.LeaveRepo{DB: pool}
	otps := postgres.OTPRepo{DB: pool}
	sessions := postgres.SessionRepo{DB: pool}

	// ConsoleSender logs OTP codes instead of sending real email/SMS — fine
	// for local dev and this checkpoint's docker-compose stack. Swap this
	// one line for a real provider (SES/SNS/Twilio/etc, implementing
	// app.OTPSender) before running against real members.
	var sender app.OTPSender = auth.ConsoleSender{}

	deps := httpapi.Deps{
		Auth: app.AuthService{
			Members: members, Secretaries: secretaries, CentralAdmins: centralAdmins,
			OTPs: otps, Sessions: sessions, Sender: sender,
		},
		Entries: app.EntryService{Hostels: hostels, Members: members, Entries: entries, Leaves: leaves},
		Leaves:  app.LeaveService{Members: members, Leaves: leaves},
		Billing: app.BillingService{Hostels: hostels, Members: members, Entries: entries, Leaves: leaves},
		Secretary: app.SecretaryService{Hostels: hostels, Members: members},
		Admin:     app.AdminService{Hostels: hostels, Secretaries: secretaries},
		Hostels:   hostels,
		Members:   members,
		Sessions:  sessions,
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      httpapi.NewRouter(deps),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
