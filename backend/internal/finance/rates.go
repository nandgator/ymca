package finance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	amountPattern   = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Rate is Finance's price for one unit of a consumption type.
type Rate struct {
	ID       string  `json:"id"`
	TypeID   string  `json:"consumption_type_id"`
	Amount   string  `json:"amount"`
	Currency string  `json:"currency"`
	From     string  `json:"from"`
	To       *string `json:"to"`
}

// NewRate is what a caller supplies. From and To are inclusive days; a nil To
// is open-ended.
type NewRate struct {
	Amount   string
	Currency string
	From     string
	To       *string
}

// CreateRate prices a unit of consumption over a range of days (A3.11).
func CreateRate(ctx context.Context, tx pgx.Tx, tenantID, typeID string, in NewRate) (Rate, error) {
	if !amountPattern.MatchString(in.Amount) {
		return Rate{}, fmt.Errorf("%w: amount must be a non-negative decimal with at most two places", ErrInvalid)
	}
	if !currencyPattern.MatchString(in.Currency) {
		return Rate{}, fmt.Errorf("%w: currency must be an ISO 4217 code", ErrInvalid)
	}
	lo, err := time.Parse("2006-01-02", in.From)
	if err != nil {
		return Rate{}, fmt.Errorf("%w: from is not a YYYY-MM-DD date", ErrInvalid)
	}
	var hi *time.Time
	if in.To != nil {
		end, err := time.Parse("2006-01-02", *in.To)
		if err != nil || end.Before(lo) {
			return Rate{}, fmt.Errorf("%w: to is not a date on or after from", ErrInvalid)
		}
		next := end.AddDate(0, 0, 1)
		hi = &next
	}

	r := Rate{From: in.From, To: in.To}
	err = tx.QueryRow(ctx, `
		INSERT INTO consumption_rate
		    (id, tenant_id, consumption_type_id, amount, currency, valid_during)
		SELECT gen_random_uuid(), t.tenant_id, t.id, $3::numeric, $4,
		       daterange($5::date, $6::date, '[)')
		  FROM consumption_type t
		 WHERE t.id = $2::uuid AND t.tenant_id = $1
		RETURNING id::text, consumption_type_id::text, amount::text, currency
	`, tenantID, typeID, in.Amount, in.Currency, lo, hi).
		Scan(&r.ID, &r.TypeID, &r.Amount, &r.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rate{}, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
		return Rate{}, ErrRateOverlaps
	}
	if err != nil {
		return Rate{}, fmt.Errorf("finance: create rate: %w", err)
	}
	return r, nil
}
