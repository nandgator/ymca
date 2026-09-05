// middleware.go builds the request pipeline: panic recovery, a request id,
// structured logging, authentication (A3.2) and the tenant-match gate
// ADR-105 requires before routing.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/audit"
	"github.com/nandgator/ymca/backend/internal/auth"
)

// RequestIDHeader is D9's carrier, both incoming (when well-formed) and
// outgoing (always).
const RequestIDHeader = "X-Request-Id"

type contextKey int

const (
	principalKey contextKey = iota
	requestIDKey
)

// PrincipalFromContext returns the Principal Authenticate placed on the
// request context, if any.
func PrincipalFromContext(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey).(auth.Principal)
	return p, ok
}

func withPrincipal(ctx context.Context, p auth.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// RequestIDFromContext returns the request id RequestID placed on the
// context, or "" if that middleware never ran.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// Middleware wraps a handler with one pipeline stage.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares around h, outermost first: Chain(h, a, b)
// runs a, then b, then h, for every request.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Recover turns a panic anywhere downstream into a 500 in A3.4's shape.
// The panic and its stack go to the structured log — never into the
// response body, which is the property this step's tests pin down.
//
// The request id is read from the response header rather than the request
// context: RequestID sets that header before calling next, so it is
// present on w regardless of where downstream a panic occurs, even though
// Recover's own r has not been re-wrapped with the enriched context that
// RequestID hands only to its next handler.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(debug.Stack()),
						"request_id", w.Header().Get(RequestIDHeader),
						"method", r.Method,
						"path", r.URL.Path,
					)
					WriteError(w, CodeInternal, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID honours an incoming X-Request-Id if it is well-formed, and
// generates one from crypto/rand otherwise (D9). It reaches the structured
// log and the audit context via RequestIDFromContext.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !wellFormedRequestID(id) {
				id = generateRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
		})
	}
}

// wellFormedRequestID is deliberately narrow: visible ASCII only, no
// whitespace or control characters, short enough that it cannot be used to
// smuggle anything through a log line or a jsonb column. D9 requires the
// check without defining "well-formed" further; this is the definition.
func wellFormedRequestID(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails if the OS entropy source itself is
		// gone, which nothing downstream can recover from either. A fixed
		// marker keeps the request identifiable rather than panicking
		// mid-request over a missing correlation id.
		return "requestid-unavailable"
	}
	return hex.EncodeToString(b)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logging writes one structured line per request, after it completes.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// Authenticate resolves the bearer token through authenticator (A3.2),
// placing the resulting Principal on the request context. Every failure —
// missing header, malformed header, an Authenticator's ErrUnauthenticated —
// is the same 401 unauthenticated to the caller (D3, 8.8); only
// ErrTokenExpired gets its own code, because a client can react to it
// differently.
//
// D8: an authentication failure has no principal and no established
// tenant, so it cannot satisfy audit_event's tenant_isolation policy — a
// null tenant_id row is uninsertable from a tenant connection. It goes to
// the structured log instead; this is that log line.
func Authenticate(authenticator auth.Authenticator, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, prefix) || strings.TrimSpace(strings.TrimPrefix(h, prefix)) == "" {
				logger.WarnContext(r.Context(), "authentication failed",
					"reason", "missing or malformed Authorization header",
					"request_id", RequestIDFromContext(r.Context()),
				)
				WriteError(w, CodeUnauthenticated, "missing or malformed Authorization header")
				return
			}
			token := strings.TrimPrefix(h, prefix)

			principal, err := authenticator.Authenticate(r.Context(), token)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrTokenExpired):
					logger.WarnContext(r.Context(), "authentication failed",
						"reason", "token expired",
						"request_id", RequestIDFromContext(r.Context()),
					)
					WriteError(w, CodeTokenExpired, "token expired")
				case errors.Is(err, auth.ErrUnauthenticated):
					logger.WarnContext(r.Context(), "authentication failed",
						"reason", "invalid credentials",
						"request_id", RequestIDFromContext(r.Context()),
					)
					WriteError(w, CodeUnauthenticated, "invalid credentials")
				default:
					logger.ErrorContext(r.Context(), "authentication error",
						"error", err,
						"request_id", RequestIDFromContext(r.Context()),
					)
					WriteError(w, CodeInternal, "authentication failed")
				}
				return
			}

			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))
		})
	}
}

// tenantTxRunner is the minimum of internal/db.DB that TenantMatch depends
// on, narrowed so the mismatch path is unit-testable without a live
// Postgres. *db.DB satisfies it already.
type tenantTxRunner interface {
	InTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error
}

// platformExecer is the minimum internal/db this file needs to record a
// platform-plane refusal — narrowed here for the same reason
// tenantTxRunner is. *pgxpool.Pool satisfies it, so callers pass
// db.Pool(): platform_audit_event has no tenant_id and no policy, so
// there is nothing for InTenantTx to set (ADR-112).
type platformExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PlatformOnly is the platform half of ADR-111's pair. It admits a request
// only if the credential is a platform credential.
//
// A tenant-bound credential reaching a platform route is refused here,
// before any handler runs. A3.1's invariant 9 has always said a
// platform-plane route reaching a tenant object is a defect; this is the
// converse, which nothing stated until R10 — a tenant credential must not
// reach the platform plane either. The two gates are mutually exclusive: a
// request satisfies exactly one of PlatformOnly and TenantMatch, never both
// and never neither.
//
// The refusal is 403 rather than 404. The platform routes are a fixed,
// public part of the API surface (A3.1) — their existence is not the
// information 8.8 protects, and pretending they are absent would make an
// operator's own misconfiguration indistinguishable from a typo.
func PlatformOnly(db platformExecer, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				// Authenticate must run before PlatformOnly on every route
				// that uses it. Reaching here without a principal is a
				// wiring defect, not a caller error — the same reading
				// TenantMatch takes.
				WriteError(w, CodeInternal, "platform gate ran before authentication")
				return
			}

			if principal.Plane != auth.PlanePlatform {
				reqID := RequestIDFromContext(r.Context())
				if err := audit.WritePlatformDeny(r.Context(), db, audit.DenyEvent{
					ActorPrincipalID: principal.ID,
					Action:           "request.plane_mismatch",
					ObjectType:       "platform",
					Severity:         "INFO",
					Reason:           "tenant_credential_on_platform_route",
					RequestID:        reqID,
				}); err != nil {
					logger.ErrorContext(r.Context(), "platform audit write failed",
						"error", err, "request_id", reqID)
				}
				WriteError(w, CodeForbidden, "this credential is not a platform credential")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TenantMatch enforces ADR-105: the tenant named in the path must equal
// the tenant the token named. It is applied per-route, wrapping the
// specific handler after ServeMux has matched {tenant} — a middleware
// wrapping the whole mux cannot see r.PathValue results, since ServeMux
// only populates them while resolving the specific route, which happens
// after any handler wrapping the mux itself has already run.
//
// Unlike an authentication failure, a mismatch here does have a principal
// and a nameable tenant — the one in the path — so it is audited normally
// (D8) rather than only logged.
func TenantMatch(runner tenantTxRunner, logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pathTenant := r.PathValue("tenant")
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				// Authenticate must run before TenantMatch on every route
				// that uses it. Reaching here without a principal is a
				// wiring defect, not a caller error.
				WriteError(w, CodeInternal, "tenant match ran before authentication")
				return
			}

			// ADR-111, stated explicitly rather than left to arithmetic.
			// A platform credential names no tenant, so the comparison
			// below already refused it — but only because "" never equals
			// a real path segment. That is a coincidence of empty strings
			// doing the work of a security boundary. Naming the plane
			// makes the refusal survive someone later deciding a platform
			// credential should carry a tenant for convenience.
			if principal.Plane != auth.PlaneTenant {
				reqID := RequestIDFromContext(r.Context())
				logger.WarnContext(r.Context(), "platform credential on a tenant route",
					"principal_id", principal.ID, "path_tenant", pathTenant,
					"request_id", reqID)
				WriteError(w, CodeForbidden, "this credential is not a tenant credential")
				return
			}

			if pathTenant == "" || pathTenant != principal.TenantID {
				reqID := RequestIDFromContext(r.Context())
				err := runner.InTenantTx(r.Context(), pathTenant, func(tx pgx.Tx) error {
					return audit.WriteDeny(r.Context(), tx, pathTenant, audit.DenyEvent{
						ActorPrincipalID: principal.ID,
						Action:           "request.tenant_mismatch",
						ObjectType:       "tenant",
						ObjectID:         pathTenant,
						Severity:         "INFO",
						Reason:           "path_tenant_ne_token_tenant",
						RequestID:        reqID,
					})
				})
				if err != nil {
					logger.ErrorContext(r.Context(), "audit write failed", "error", err, "request_id", reqID)
				}
				WriteError(w, CodeTenantMismatch, "path tenant does not match the authenticated principal's tenant")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
