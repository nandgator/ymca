package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ymca/mess-backend/internal/domain"
)

type HostelRepo struct{ DB *pgxpool.Pool }

func (r HostelRepo) Create(ctx context.Context, h domain.Hostel) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	if _, err := tx.Exec(ctx, `insert into hostels (id, name) values ($1, $2)`, h.ID, h.Name); err != nil {
		return fmt.Errorf("inserting hostel: %w", err)
	}
	menuDays := h.Policy.Menu.Days[:]
	if _, err := tx.Exec(ctx, `
		insert into hostel_policies
			(hostel_id, flat_monthly_fee_paise, non_veg_surcharge_paise, daily_deduction_paise, long_leave_threshold_days, menu_days)
		values ($1, $2, $3, $4, $5, $6)`,
		h.ID, h.Policy.FlatMonthlyFeeINR, h.Policy.NonVegSurchargeINR, h.Policy.DailyDeductionINR, h.Policy.LongLeaveThresholdDays, menuDays,
	); err != nil {
		return fmt.Errorf("inserting hostel policy: %w", err)
	}
	return tx.Commit(ctx)
}

func (r HostelRepo) Get(ctx context.Context, id uuid.UUID) (domain.Hostel, error) {
	var h domain.Hostel
	h.ID = id
	var menuDays []string

	err := r.DB.QueryRow(ctx, `
		select h.name, p.flat_monthly_fee_paise, p.non_veg_surcharge_paise, p.daily_deduction_paise, p.long_leave_threshold_days, p.menu_days
		from hostels h
		join hostel_policies p on p.hostel_id = h.id
		where h.id = $1`, id,
	).Scan(&h.Name, &h.Policy.FlatMonthlyFeeINR, &h.Policy.NonVegSurchargeINR, &h.Policy.DailyDeductionINR, &h.Policy.LongLeaveThresholdDays, &menuDays)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Hostel{}, fmt.Errorf("hostel %s: %w", id, err)
		}
		return domain.Hostel{}, fmt.Errorf("querying hostel: %w", err)
	}
	copy(h.Policy.Menu.Days[:], menuDays)

	items, err := r.listOptionalItems(ctx, id)
	if err != nil {
		return domain.Hostel{}, err
	}
	h.Policy.OptionalItems = items
	return h, nil
}

func (r HostelRepo) listOptionalItems(ctx context.Context, hostelID uuid.UUID) ([]domain.OptionalItem, error) {
	rows, err := r.DB.Query(ctx, `select id, name, price_paise from optional_items where hostel_id = $1 and active order by name`, hostelID)
	if err != nil {
		return nil, fmt.Errorf("querying optional items: %w", err)
	}
	defer rows.Close()

	var items []domain.OptionalItem
	for rows.Next() {
		var it domain.OptionalItem
		if err := rows.Scan(&it.ID, &it.Name, &it.PriceINR); err != nil {
			return nil, fmt.Errorf("scanning optional item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// List returns hostel id+name only, deliberately — CentralAdmin (the only
// caller of List) never sees policy/pricing per CONTEXT.md.
func (r HostelRepo) List(ctx context.Context) ([]domain.Hostel, error) {
	rows, err := r.DB.Query(ctx, `select id, name from hostels order by name`)
	if err != nil {
		return nil, fmt.Errorf("querying hostels: %w", err)
	}
	defer rows.Close()

	var out []domain.Hostel
	for rows.Next() {
		var h domain.Hostel
		if err := rows.Scan(&h.ID, &h.Name); err != nil {
			return nil, fmt.Errorf("scanning hostel: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r HostelRepo) UpdatePolicy(ctx context.Context, hostelID uuid.UUID, policy domain.HostelPolicy) error {
	menuDays := policy.Menu.Days[:]
	_, err := r.DB.Exec(ctx, `
		update hostel_policies set
			flat_monthly_fee_paise = $2,
			non_veg_surcharge_paise = $3,
			daily_deduction_paise = $4,
			long_leave_threshold_days = $5,
			menu_days = $6,
			updated_at = now()
		where hostel_id = $1`,
		hostelID, policy.FlatMonthlyFeeINR, policy.NonVegSurchargeINR, policy.DailyDeductionINR, policy.LongLeaveThresholdDays, menuDays,
	)
	if err != nil {
		return fmt.Errorf("updating hostel policy: %w", err)
	}
	return nil
}

func (r HostelRepo) AddOptionalItem(ctx context.Context, hostelID uuid.UUID, item domain.OptionalItem) error {
	_, err := r.DB.Exec(ctx, `insert into optional_items (id, hostel_id, name, price_paise) values ($1, $2, $3, $4)`,
		item.ID, hostelID, item.Name, item.PriceINR)
	if err != nil {
		return fmt.Errorf("inserting optional item: %w", err)
	}
	return nil
}

func (r HostelRepo) DeactivateOptionalItem(ctx context.Context, hostelID, itemID uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `update optional_items set active = false where hostel_id = $1 and id = $2`, hostelID, itemID)
	if err != nil {
		return fmt.Errorf("deactivating optional item: %w", err)
	}
	return nil
}
