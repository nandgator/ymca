package httpapi

import (
	"net/http"
	"strings"

	"github.com/ymca/mess-backend/internal/app"
	"github.com/ymca/mess-backend/internal/auth"
)

// requireAuth validates the bearer token against the session store and
// injects the resulting Actor into the request context. Handlers behind
// this middleware use actorFrom(r.Context()) — never trust any
// role/hostel/id claim from the request body itself, only from the
// session that was resolved here.
func requireAuth(sessions app.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, errorBody{Error: "missing or malformed Authorization header"})
				return
			}

			actor, found, err := sessions.Lookup(r.Context(), auth.HashSecret(token))
			if err != nil {
				writeError(w, err)
				return
			}
			if !found {
				writeJSON(w, http.StatusUnauthorized, errorBody{Error: "session is invalid or has expired"})
				return
			}

			next.ServeHTTP(w, r.WithContext(withActor(r.Context(), actor)))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
