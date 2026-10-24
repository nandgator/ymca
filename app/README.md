# app

The Flutter client: one app for members and staff. What a person sees is
decided by what `GET /t/{tenant}/me` says they may do (A3.9) — never by a role
the client guessed — so a member sees Meals and Bills, a front desk adds
Members, a bursar adds Invoices, and an administrator adds Close.

```txt
lib/api/models.dart       wire models; money is a decimal STRING end to end
lib/api/ymca_api.dart     the interface screens depend on, and A3.4's error
lib/api/http_ymca_api.dart  the HTTP implementation
lib/state/                the session, and what the server says it may do
lib/ui/support.dart       attempt(), PagedList — one way to fail, one way to page
lib/ui/                   meals, bills, members, admission, invoices (with credit
                          notes), close
lib/util/                 idempotency keys, and the tenant's calendar (ADR-119)
```

## Run

```sh
flutter pub get
flutter run -d linux            # or -d chrome, or an Android device
```

The sign-in page takes a server, an association id and an access token. Today
the token is the development authenticator's (ADR-106); mint one with
`go run -tags dev ./cmd/api mint-token <idp-subject> <tenant>` in `backend/`.
A web build needs its own origin listed in the server's `YMCA_CORS_ORIGINS`.

## Test

```sh
flutter analyze
flutter test                    # widget tests against a fake; no server needed
```

`test/live_api_test.dart` is the Dart half of a cross-language contract test.
It skips on its own and runs when the Go test
`TestFlutterClientAgreesWithTheServer` boots the real routes, seeds a hostel and
runs it — see `backend/README.md`. The widget tests use a fake that agrees with
the client by construction, so only that test can catch the client and the
server drifting apart.

## Four things that were easy to get wrong

**An idempotency key belongs to a request, not to a screen.** A3.6's header is
what makes a retried meal one meal. A screen makes a key when the user starts
an action and reuses it while they retry the _same_ action — and makes a new one
the moment the day, the amount or the number changes, because the same key with
a different body is `idempotency_key_reused`. All three workers that wrote
screens made the key once and kept it. Each place is drift-tested.

**Providers must sit above the Navigator.** Providers placed inside
`MaterialApp.home` are invisible to every pushed route and bottom sheet, so the
first tap on a bill throws. `YmcaApp` provides the API in `MaterialApp.builder`,
and `test/shell_test.dart` drives the real app through a navigation to prove it.
Tests that mount a screen inside `home:` cannot see this failure.

**"Today" is the tenant's.** A meal eaten at 1am in Mumbai belongs to that day,
and the device's zone and UTC both get it wrong. `/me` reports the tenant's UTC
offset; `Calendar` (`lib/util/dates.dart`) turns an instant into the tenant's day
with it, and every screen takes its day from `calendarOf(context)`. A date picker
hands back the device's local midnight, which `pickedDay` keeps as the calendar
day it names; converting it to UTC first lands a day early east of Greenwich.
Tests run a +14 and a -11 offset because one of them is always on a different
date from UTC.

**The server's message is for a developer.** A3.4 says so. Screens show
`ApiException.userMessage`, which maps the codes a person can act on, and an
expired token signs the user out instead of showing an error.
