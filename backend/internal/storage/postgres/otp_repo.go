package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type OTPRepo struct{ DB *pgxpool.Pool }

func (r OTPRepo) Issue(ctx context.Context, subjectType domain.Role, subjectID uuid.UUID, channel, destination, codeHash string, expiresAt int64) error {
	_, err := r.DB.Exec(ctx, `
		insert into otp_codes (subject_type, subject_id, code_hash, channel, destination, expires_at)
		values ($1, $2, $3, $4, $5, $6)`,
		string(subjectType), subjectID, codeHash, channel, destination, time.Unix(expiresAt, 0).UTC(),
	)
	if err != nil {
		return fmt.Errorf("inserting otp: %w", err)
	}
	return nil
}

// VerifyAndConsume matches against the most recently issued, unconsumed,
// unexpired code for this subject. A match is marked consumed immediately
// so it can never be replayed. A mismatch increments attempt_count on the
// live code as a hook for future throttling (not enforced yet — see
// CONTEXT.md's out-of-scope list; this is schema-ready, not wired to a
// lockout policy).
func (r OTPRepo) VerifyAndConsume(ctx context.Context, subjectType domain.Role, subjectID uuid.UUID, codeHash string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `
		update otp_codes
		set consumed_at = now()
		where id = (
			select id from otp_codes
			where subject_type = $1 and subject_id = $2 and code_hash = $3
				and consumed_at is null and expires_at > now()
			order by created_at desc
			limit 1
		)`,
		string(subjectType), subjectID, codeHash,
	)
	if err != nil {
		return false, fmt.Errorf("consuming otp: %w", err)
	}
	if tag.RowsAffected() == 0 {
		_, _ = r.DB.Exec(ctx, `
			update otp_codes set attempt_count = attempt_count + 1
			where id = (
				select id from otp_codes
				where subject_type = $1 and subject_id = $2 and consumed_at is null and expires_at > now()
				order by created_at desc limit 1
			)`, string(subjectType), subjectID)
		return false, nil
	}
	return true, nil
}
