# Backend — YMCA Mess Management

Go 1.22, chi router, Postgres via pgx. See `/CONTEXT.md` at the repo root
for the domain glossary this code is built against — read that first if
anything here seems arbitrary.

## Run locally (Docker)

```bash
cd deploy
docker compose up --build
```

This builds the backend image, starts Postgres, and applies
`backend/db/migrations/0001_init.sql` (schema) then `0002_seed.sql` (demo
hostel + one admin/secretary/member) automatically on first boot — only on
a *fresh* Postgres volume. To reset: `docker compose down -v`.

API is then at `http://localhost:8080`. Try:

```bash
curl http://localhost:8080/healthz

curl -X POST http://localhost:8080/auth/otp/request \
  -H 'Content-Type: application/json' \
  -d '{"role":"MEMBER","login_id":"YMCA-2026-0001","channel":"EMAIL"}'
# the OTP code is printed in the backend container logs (ConsoleSender) —
# `docker compose logs backend` — since no real email/SMS provider is
# wired up for local dev.

curl -X POST http://localhost:8080/auth/otp/verify \
  -H 'Content-Type: application/json' \
  -d '{"role":"MEMBER","login_id":"YMCA-2026-0001","code":"<code from logs>"}'
# returns a bearer token — use it as `Authorization: Bearer <token>` on
# every /member, /secretary, /admin request.
```

## Run locally (without Docker)

Needs Go 1.22+ and a Postgres instance.

```bash
cd backend
go mod tidy          # one-time — resolves dependencies, generates go.sum.
                      # Needs network access; this repo ships without a
                      # go.sum because it was generated in a sandboxed
                      # environment with no network egress.
psql "$DATABASE_URL" -f db/migrations/0001_init.sql
psql "$DATABASE_URL" -f db/migrations/0002_seed.sql   # optional demo data
DATABASE_URL=postgres://... PORT=8080 go run ./cmd/api
```

Run the domain layer's unit tests (no DB needed — pure logic):

```bash
go test ./internal/domain/...
```

## Swapping in a real OTP provider

`cmd/api/main.go` wires `auth.ConsoleSender` (logs codes instead of
sending them) as the `app.OTPSender`. Implement the same two-method
interface against SES/SNS/Twilio/etc and swap that one line before
onboarding real members.

## Layout

```
cmd/api/            entry point — wiring only, no business logic
internal/domain/     pure domain model + billing math (deep module, unit tested)
internal/app/        orchestration services + port interfaces (the seams)
internal/storage/postgres/   pgx adapters implementing those ports
internal/auth/        OTP/token generation + hashing (no DB, no transport)
internal/httpapi/     chi routes, DTOs, auth middleware, error mapping
db/migrations/        schema (0001) + local-dev-only seed data (0002)
```
