package httpx

import (
	"net/http"
	"strings"
)

// CORS lets a browser client served from another origin call this API — which
// the Flutter web build is, and a native build is not. It is closed by default:
// with no allowed origins it adds nothing and a cross-origin browser request is
// refused by the browser, exactly as before.
//
// Three decisions, each of which is the easy one to get wrong:
//
//   - An origin is echoed only if it is on the list, never reflected blindly
//     and never "*". The credential is a bearer token in a header, not a
//     cookie, so reflecting an arbitrary origin would not by itself leak a
//     session — but a wildcard is a statement the deployment should have to
//     make deliberately, by listing it.
//   - A preflight is answered here, before Authenticate. A browser sends
//     OPTIONS without the Authorization header, so authenticating it would
//     refuse every cross-origin request before it was made. It reaches no
//     handler and no data.
//   - Idempotency-Key and X-Request-Id are in Allow-Headers and
//     Expose-Headers respectively: without the first the browser strips the
//     header A3.6 depends on, silently, and every retry becomes a new request.
func CORS(allowed []string) Middleware {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o = strings.TrimSpace(o); o != "" {
			set[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		if len(set) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && set[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Expose-Headers", RequestIDHeader)
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					h.Set("Access-Control-Allow-Headers",
						"Authorization, Content-Type, Idempotency-Key, "+RequestIDHeader)
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
