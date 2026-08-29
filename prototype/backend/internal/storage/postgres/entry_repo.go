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

type EntryRepo struct{ DB *pgxpool.Pool }

// Upsert relies on the (member_id, entry_date, meal_type) unique
// constraint to implement "create, or same-day edit" as a single
// operation — the domain layer already guarantees the same-day rule
// before this is ever called, so here it's pure persistence.
func (r EntryRepo) Upsert(ctx context.Context, e domain.Entry) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var entryID uuid.UUID
	err = tx.QueryRow(ctx, `
		insert into entries (id, member_id, hostel_id, entry_date, meal_type, non_veg)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (member_id, entry_date, meal_type)
		do update set non_veg = excluded.non_veg, updated_at = now()
		returning id`,
		e.ID, e.MemberID, e.HostelID, e.Date.Time(), e.MealType, e.NonVeg,
	).Scan(&entryID)
	if err != nil {
		return fmt.Errorf("upserting entry: %w", err)
	}

	if _, err := tx.Exec(ctx, `delete from entry_optional_items where entry_id = $1`, entryID); err != nil {
		return fmt.Errorf("clearing optional items: %w", err)
	}
	for _, itemID := range e.OptionalItemIDs {
		if _, err := tx.Exec(ctx, `insert into entry_optional_items (entry_id, optional_item_id) values ($1, $2)`, entryID, itemID); err != nil {
			return fmt.Errorf("linking optional item: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r EntryRepo) Get(ctx context.Context, memberID uuid.UUID, date domain.Day, meal domain.MealType) (domain.Entry, bool, error) {
	var e domain.Entry
	var entryDate time.Time
	err := r.DB.QueryRow(ctx, `
		select id, member_id, hostel_id, entry_date, meal_type, non_veg, created_at, updated_at
		from entries where member_id = $1 and entry_date = $2 and meal_type = $3`,
		memberID, date.Time(), meal,
	).Scan(&e.ID, &e.MemberID, &e.HostelID, &entryDate, &e.MealType, &e.NonVeg, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Entry{}, false, nil
		}
		return domain.Entry{}, false, fmt.Errorf("querying entry: %w", err)
	}
	e.Date = domain.DayOf(entryDate)

	entries, err := r.attachOptionalItems(ctx, []domain.Entry{e})
	if err != nil {
		return domain.Entry{}, false, err
	}
	return entries[0], true, nil
}

func (r EntryRepo) ListForMemberMonth(ctx context.Context, memberID uuid.UUID, year, month int) ([]domain.Entry, error) {
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, entry_date, meal_type, non_veg, created_at, updated_at
		from entries
		where member_id = $1 and extract(year from entry_date) = $2 and extract(month from entry_date) = $3
		order by entry_date, meal_type`,
		memberID, year, month,
	)
	if err != nil {
		return nil, fmt.Errorf("querying entries: %w", err)
	}
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	return r.attachOptionalItems(ctx, entries)
}

func (r EntryRepo) ListForHostelMonth(ctx context.Context, hostelID uuid.UUID, year, month int) ([]domain.Entry, error) {
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, entry_date, meal_type, non_veg, created_at, updated_at
		from entries
		where hostel_id = $1 and extract(year from entry_date) = $2 and extract(month from entry_date) = $3
		order by entry_date, meal_type`,
		hostelID, year, month,
	)
	if err != nil {
		return nil, fmt.Errorf("querying entries: %w", err)
	}
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	return r.attachOptionalItems(ctx, entries)
}

func (r EntryRepo) ListForHostelDate(ctx context.Context, hostelID uuid.UUID, date domain.Day) ([]domain.Entry, error) {
	rows, err := r.DB.Query(ctx, `
		select id, member_id, hostel_id, entry_date, meal_type, non_veg, created_at, updated_at
		from entries
		where hostel_id = $1 and entry_date = $2
		order by meal_type`,
		hostelID, date.Time(),
	)
	if err != nil {
		return nil, fmt.Errorf("querying entries: %w", err)
	}
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	return r.attachOptionalItems(ctx, entries)
}

func scanEntries(rows pgx.Rows) ([]domain.Entry, error) {
	defer rows.Close()
	var out []domain.Entry
	for rows.Next() {
		var e domain.Entry
		var entryDate time.Time
		if err := rows.Scan(&e.ID, &e.MemberID, &e.HostelID, &entryDate, &e.MealType, &e.NonVeg, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		e.Date = domain.DayOf(entryDate)
		out = append(out, e)
	}
	return out, rows.Err()
}

// attachOptionalItems batches the entry->optional-item lookup into one
// query regardless of how many entries were passed in, to avoid N+1.
func (r EntryRepo) attachOptionalItems(ctx context.Context, entries []domain.Entry) ([]domain.Entry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	ids := make([]uuid.UUID, len(entries))
	index := make(map[uuid.UUID]int, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
		index[e.ID] = i
	}

	rows, err := r.DB.Query(ctx, `select entry_id, optional_item_id from entry_optional_items where entry_id = any($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("querying optional item links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entryID, itemID uuid.UUID
		if err := rows.Scan(&entryID, &itemID); err != nil {
			return nil, fmt.Errorf("scanning optional item link: %w", err)
		}
		if i, ok := index[entryID]; ok {
			entries[i].OptionalItemIDs = append(entries[i].OptionalItemIDs, itemID)
		}
	}
	return entries, rows.Err()
}
