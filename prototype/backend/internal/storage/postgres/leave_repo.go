package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type LeaveRepo struct{ DB *pgxpool.Pool }

func (r LeaveRepo) Create(ctx context.Context, l domain.Leave) error {
	_, err := r.DB.Exec(ctx, `
		insert into leaves (id, member_id, hostel_id, start_date, end_date)
		values ($1, $2, $3, $4, $5)`,
		l.ID, l.MemberID, l.HostelID, l.StartDate.Time(), l.EndDate.Time(),
	)
	if err != nil {
		return fmt.Errorf("inserting leave: %w", err)
	}
	return nil
}

func (r LeaveRepo) ListForMember(ctx context.Context, memberID uuid.UUID) ([]domain.Leave, error) {
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, start_date, end_date
		from leaves where member_id = $1 order by start_date`, memberID)
	if err != nil {
		return nil, fmt.Errorf("querying leaves: %w", err)
	}
	return scanLeaves(rows)
}

// ListForMemberMonth returns any leave whose date range *overlaps* the
// given calendar month, not just ones that start within it — a leave
// spanning a month boundary must be visible to both months' bills.
func (r LeaveRepo) ListForMemberMonth(ctx context.Context, memberID uuid.UUID, year, month int) ([]domain.Leave, error) {
	start, end := monthRange(year, month)
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, start_date, end_date
		from leaves
		where member_id = $1 and start_date <= $3 and end_date >= $2
		order by start_date`,
		memberID, start, end,
	)
	if err != nil {
		return nil, fmt.Errorf("querying leaves: %w", err)
	}
	return scanLeaves(rows)
}

func (r LeaveRepo) ListForHostelMonth(ctx context.Context, hostelID uuid.UUID, year, month int) ([]domain.Leave, error) {
	start, end := monthRange(year, month)
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, start_date, end_date
		from leaves
		where hostel_id = $1 and start_date <= $3 and end_date >= $2
		order by start_date`,
		hostelID, start, end,
	)
	if err != nil {
		return nil, fmt.Errorf("querying leaves: %w", err)
	}
	return scanLeaves(rows)
}

func (r LeaveRepo) CoveringDate(ctx context.Context, memberID uuid.UUID, date domain.Day) (domain.Leave, bool, error) {
	var l domain.Leave
	var start, end time.Time
	err := r.DB.QueryRow(ctx, `
		select id, member_id, hostel_id, start_date, end_date
		from leaves
		where member_id = $1 and start_date <= $2 and end_date >= $2
		limit 1`,
		memberID, date.Time(),
	).Scan(&l.ID, &l.MemberID, &l.HostelID, &start, &end)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Leave{}, false, nil
		}
		return domain.Leave{}, false, fmt.Errorf("querying leave: %w", err)
	}
	l.StartDate = domain.DayOf(start)
	l.EndDate = domain.DayOf(end)
	return l, true, nil
}

func scanLeaves(rows pgx.Rows) ([]domain.Leave, error) {
	defer rows.Close()
	var out []domain.Leave
	for rows.Next() {
		var l domain.Leave
		var start, end time.Time
		if err := rows.Scan(&l.ID, &l.MemberID, &l.HostelID, &start, &end); err != nil {
			return nil, fmt.Errorf("scanning leave: %w", err)
		}
		l.StartDate = domain.DayOf(start)
		l.EndDate = domain.DayOf(end)
		out = append(out, l)
	}
	return out, rows.Err()
}

func monthRange(year, month int) (time.Time, time.Time) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1) // last calendar day of the month
	return start, end
}
