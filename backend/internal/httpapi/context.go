package httpapi

import (
	"context"

	"github.com/ymca/mess-backend/internal/domain"
)

type ctxKey int

const actorCtxKey ctxKey = 1

func withActor(ctx context.Context, a domain.Actor) context.Context {
	return context.WithValue(ctx, actorCtxKey, a)
}

// actorFrom returns the authenticated Actor. Only call this on routes
// mounted behind requireAuth — it panics on missing context deliberately,
// so a route wired without the middleware fails loudly in testing rather
// than silently treating every request as anonymous.
func actorFrom(ctx context.Context) domain.Actor {
	a, ok := ctx.Value(actorCtxKey).(domain.Actor)
	if !ok {
		panic("httpapi: actorFrom called on a route not behind requireAuth")
	}
	return a
}
