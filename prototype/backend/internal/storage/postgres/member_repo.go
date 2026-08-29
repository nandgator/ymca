package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type MemberRepo struct{ DB *pgxpool.Pool }

func (r MemberRepo) Create(ctx context.Context, m domain.Member) error {
	_, err := r.DB.Exec(ctx, `
		insert into members (id, hostel_id, member_id, name, email, mobile)
		values ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.HostelID, m.MemberID, m.Name, m.Email, m.Mobile,
	)
	if err != nil {
		return fmt.Errorf("inserting member: %w", err)
	}
	return nil
}

func (r MemberRepo) Get(ctx context.Context, id uuid.UUID) (domain.Member, error) {
	return r.scanOne(ctx, `select id, hostel_id, member_id, name, email, mobile from members where id = $1`, id)
}

func (r MemberRepo) GetByMemberID(ctx context.Context, memberID string) (domain.Member, error) {
	return r.scanOne(ctx, `select id, hostel_id, member_id, name, email, mobile from members where member_id = $1`, memberID)
}

func (r MemberRepo) scanOne(ctx context.Context, query string, arg any) (domain.Member, error) {
	var m domain.Member
	err := r.DB.QueryRow(ctx, query, arg).Scan(&m.ID, &m.HostelID, &m.MemberID, &m.Name, &m.Email, &m.Mobile)
	if err != nil {
		return domain.Member{}, fmt.Errorf("querying member: %w", err)
	}
	return m, nil
}

func (r MemberRepo) ListByHostel(ctx context.Context, hostelID uuid.UUID) ([]domain.Member, error) {
	rows, err := r.DB.Query(ctx, `select id, hostel_id, member_id, name, email, mobile from members where hostel_id = $1 order by name`, hostelID)
	if err != nil {
		return nil, fmt.Errorf("querying roster: %w", err)
	}
	defer rows.Close()

	var out []domain.Member
	for rows.Next() {
		var m domain.Member
		if err := rows.Scan(&m.ID, &m.HostelID, &m.MemberID, &m.Name, &m.Email, &m.Mobile); err != nil {
			return nil, fmt.Errorf("scanning member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
