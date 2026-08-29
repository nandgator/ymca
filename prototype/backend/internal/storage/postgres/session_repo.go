package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type SessionRepo struct{ DB *pgxpool.Pool }

func (r SessionRepo) Create(ctx context.Context, actor domain.Actor, tokenHash string, expiresAt int64) error {
	_, err := r.DB.Exec(ctx, `
		insert into sessions (subject_type, subject_id, hostel_id, token_hash, expires_at)
		values ($1, $2, $3, $4, $5)`,
		string(actor.Role), actor.ID, actor.HostelID, tokenHash, time.Unix(expiresAt, 0).UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}
	return nil
}

func (r SessionRepo) Lookup(ctx context.Context, tokenHash string) (domain.Actor, bool, error) {
	var a domain.Actor
	var role string
	err := r.DB.QueryRow(ctx, `
		select subject_type, subject_id, hostel_id
		from sessions
		where token_hash = $1 and expires_at > now()`,
		tokenHash,
	).Scan(&role, &a.ID, &a.HostelID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Actor{}, false, nil
		}
		return domain.Actor{}, false, fmt.Errorf("querying session: %w", err)
	}
	a.Role = domain.Role(role)
	return a, true, nil
}

func (r SessionRepo) Revoke(ctx context.Context, tokenHash string) error {
	_, err := r.DB.Exec(ctx, `delete from sessions where token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}
