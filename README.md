# YMCA Mess Management

Digitalizes a YMCA hostel's paper mess register. Members currently write
what they had for breakfast/dinner in a notebook every day (mandatory
unless on registered leave); a staff member manually tallies it by hand at
month end. This replaces that with a small mobile app and a backend that
computes the bill.

**Start with [`CONTEXT.md`](./CONTEXT.md)** — the domain glossary every
other file in this repo is built against. If a naming choice or a rule
anywhere in the code looks arbitrary, it's answered there.

## Architecture

```
┌────────────────────────┐        HTTPS          ┌──────────────────────────┐
│  Mobile app (KMP +     │ ───────────────────▶ │  Caddy (reverse proxy,   │
│  Compose Multiplatform)│                       │  automatic TLS)          │
│  Android + iOS,        │                       └────────────┬─────────────┘
│  one shared UI         │                                  │
└────────────────────────┘                       ┌────────────▼─────────────┐
                                               │  Go backend (chi router)  │
                                               │  internal/domain — pure   │
                                               │  billing/entry/leave logic│
                                               │  internal/app — orchestr. │
                                               │  internal/httpapi — routes│
                                               └────────────┬─────────────┘
                                                             │ pgx
                                               ┌────────────▼─────────────┐
                                               │       Postgres            │
                                               └────────────────────────────┘
```

Every actor (Member, Secretary, CentralAdmin) uses the same mobile app —
the UI branches by role after login, rather than shipping separate apps.
Nobody self-registers; accounts are provisioned offline by a Secretary or
CentralAdmin, and login is OTP-only (no passwords anywhere in the system).

## Repo layout

```
CONTEXT.md          domain glossary — read this first
backend/            Go API — see backend/README.md to run it
mobile/              KMP + Compose Multiplatform app — see mobile/README.md
deploy/              docker-compose (local + prod) and EC2 setup — see deploy/README.md
docs/adr/            (empty — for architecture decision records if this grows)
```

## Quick start (local)

```bash
cd deploy
docker compose up --build
```

This starts Postgres + the backend, applying the schema and a demo
hostel/admin/secretary/member on first boot. Then open `mobile/` in
Android Studio and run it — it points at this local backend by default on
the Android emulator. See `backend/README.md` for example curl commands
(OTP login flow) and `mobile/README.md` for iOS setup.

## Known caveats, honestly stated

This was built in a sandboxed environment with **no network access** and
**no Go/Gradle/Xcode toolchains available** to actually compile it. Every
file was hand-reviewed for syntax and logic correctness instead of
compiler-verified. Concretely, before treating this as production-ready:

- **Backend**: run `cd backend && go mod tidy && go test ./...` — this
  resolves dependencies (generates `go.sum`, not shipped since it needs
  network) and runs the domain layer's unit tests (billing math, entry
  locking, leave classification). One real bug was already caught this way
  during writing (see `/areas/ymca-mess-monorepo.md` build notes if
  you're a future Claude session resuming this) — there may be others
  `go vet`/`go build` would catch that manual review didn't.
- **Mobile**: open `mobile/` in Android Studio and let Gradle sync — this
  resolves the Kotlin/Compose/Ktor dependencies for the first time. Two
  real generics bugs and one hardcoded-platform-URL bug were caught by
  manual review during writing; Gradle/the Kotlin compiler may catch more.
- **iOS**: needs a one-time manual Xcode project creation step — see
  `mobile/README.md`. Deliberately not shipped as a hand-authored
  `.xcodeproj`.
- **OTP delivery**: wired to a console logger (prints codes to the backend
  logs) rather than real email/SMS. Fine for testing, must be swapped
  before onboarding real members — see `deploy/README.md` step 8.

None of this changes the actual design decisions (domain model, API
shapes, architecture) — it's specifically the "did every file typecheck"
layer that a real toolchain needs to confirm.
