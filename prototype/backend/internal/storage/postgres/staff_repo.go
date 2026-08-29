package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type SecretaryRepo struct{ DB *pgxpool.Pool }

func (r SecretaryRepo) Create(ctx context.Context, s domain.Secretary) error {
	_, err := r.DB.Exec(ctx, `
		insert into secretaries (id, hostel_id, staff_id, name, email, mobile)
		values ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.HostelID, s.StaffID, s.Name, s.Email, s.Mobile,
	)
	if err != nil {
		return fmt.Errorf("inserting secretary: %w", err)
	}
	return nil
}

func (r SecretaryRepo) Get(ctx context.Context, id uuid.UUID) (domain.Secretary, error) {
	return r.scanOne(ctx, `select id, hostel_id, staff_id, name, email, mobile from secretaries where id = $1`, id)
}

func (r SecretaryRepo) GetByStaffID(ctx context.Context, staffID string) (domain.Secretary, error) {
	return r.scanOne(ctx, `select id, hostel_id, staff_id, name, email, mobile from secretaries where staff_id = $1`, staffID)
}

func (r SecretaryRepo) scanOne(ctx context.Context, query string, arg any) (domain.Secretary, error) {
	var s domain.Secretary
	err := r.DB.QueryRow(ctx, query, arg).Scan(&s.ID, &s.HostelID, &s.StaffID, &s.Name, &s.Email, &s.Mobile)
	if err != nil {
		return domain.Secretary{}, fmt.Errorf("querying secretary: %w", err)
	}
	return s, nil
}

type CentralAdminRepo struct{ DB *pgxpool.Pool }

func (r CentralAdminRepo) Get(ctx context.Context, id uuid.UUID) (domain.CentralAdmin, error) {
	return r.scanOne(ctx, `select id, staff_id, name, email, mobile from central_admins where id = $1`, id)
}

func (r CentralAdminRepo) GetByStaffID(ctx context.Context, staffID string) (domain.CentralAdmin, error) {
	return r.scanOne(ctx, `select id, staff_id, name, email, mobile from central_admins where staff_id = $1`, staffID)
}

func (r CentralAdminRepo) scanOne(ctx context.Context, query string, arg any) (domain.CentralAdmin, error) {
	var a domain.CentralAdmin
	err := r.DB.QueryRow(ctx, query, arg).Scan(&a.ID, &a.StaffID, &a.Name, &a.Email, &a.Mobile)
	if err != nil {
		return domain.CentralAdmin{}, fmt.Errorf("querying central admin: %w", err)
	}
	return a, nil
}
