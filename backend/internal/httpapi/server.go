package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/ymca/mess-backend/internal/app"
)

// Deps is every service the HTTP layer needs. Handlers stay thin — this
// struct plus context.Request is all a handler function touches; the real
// logic lives in internal/app and internal/domain.
type Deps struct {
	Auth      app.AuthService
	Entries   app.EntryService
	Leaves    app.LeaveService
	Billing   app.BillingService
	Secretary app.SecretaryService
	Admin     app.AdminService
	Hostels   app.HostelRepo
	Members   app.MemberRepo
	Sessions  app.SessionStore
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // mobile app only, no browser cookies involved — safe to be permissive
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/auth", func(r chi.Router) {
		mountAuthRoutes(r, d)
	})

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(d.Sessions))
		r.Route("/member", func(r chi.Router) {
			mountMemberRoutes(r, d)
		})
		r.Route("/secretary", func(r chi.Router) {
			mountSecretaryRoutes(r, d)
		})
		r.Route("/admin", func(r chi.Router) {
			mountAdminRoutes(r, d)
		})
	})

	return r
}
