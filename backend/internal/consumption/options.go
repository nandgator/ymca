package consumption

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/page"
)

// ErrOptionCodeTaken is 409 option_code_taken: UNIQUE (type, code).
var ErrOptionCodeTaken = errors.New("consumption: that option code is already used on this type")

// ErrInvalidOption is invalid_request: a field of an option is not one this
// catalogue accepts.
var ErrInvalidOption = errors.New("consumption: invalid option")

var moneyPattern = regexp.MustCompile(`^-?\d{1,12}(\.\d{1,2})?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var codePattern = regexp.MustCompile(`^[A-Z0-9_]{1,40}$`)

// Option is one priced addition to a consumption (05.10.5): a boiled egg with
// breakfast. The amount is a decimal string — money is never a float.
type Option struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	ChargeKind string `json:"charge_kind"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
}

// NewOption is what a caller supplies to define one.
type NewOption struct {
	Code       string
	Name       string
	ChargeKind string
	Amount     string
	Currency   string
}

// CreateOption adds an option to a type. A variant surcharge is an option
// with the variant's code (A3.10); the catalogue is the only priced thing.
func CreateOption(
	ctx context.Context, tx pgx.Tx, tenantID, typeID string, in NewOption,
) (Option, error) {
	if !codePattern.MatchString(in.Code) {
		return Option{}, fmt.Errorf("%w: code must be 1-40 characters of A-Z, 0-9 and _", ErrInvalidOption)
	}
	if in.Name == "" {
		return Option{}, fmt.Errorf("%w: name is required", ErrInvalidOption)
	}
	if in.ChargeKind != "USAGE_FEE" && in.ChargeKind != "DISCOUNT" {
		return Option{}, fmt.Errorf("%w: charge_kind must be USAGE_FEE or DISCOUNT", ErrInvalidOption)
	}
	if !moneyPattern.MatchString(in.Amount) {
		return Option{}, fmt.Errorf("%w: amount must be a decimal with at most two places", ErrInvalidOption)
	}
	if !currencyPattern.MatchString(in.Currency) {
		return Option{}, fmt.Errorf("%w: currency must be an ISO 4217 code", ErrInvalidOption)
	}

	var o Option
	err := tx.QueryRow(ctx, `
		INSERT INTO consumption_option
		    (id, tenant_id, consumption_type_id, code, name, charge_kind,
		     amount, currency, status)
		SELECT gen_random_uuid(), t.tenant_id, t.id, $3, $4, $5,
		       $6::numeric, $7, 'AVAILABLE'
		  FROM consumption_type t
		 WHERE t.id = $2::uuid AND t.tenant_id = $1
		RETURNING id::text, code, name, charge_kind, amount::text, currency, status
	`, tenantID, typeID, in.Code, in.Name, in.ChargeKind, in.Amount, in.Currency).
		Scan(&o.ID, &o.Code, &o.Name, &o.ChargeKind, &o.Amount, &o.Currency, &o.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Option{}, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return Option{}, ErrOptionCodeTaken
	}
	if err != nil {
		return Option{}, fmt.Errorf("consumption: create option: %w", err)
	}
	return o, nil
}

// ListOptions pages a type's options by code (ADR-104).
func ListOptions(
	ctx context.Context, tx pgx.Tx, tenantID, typeID string, p page.Params,
) ([]Option, error) {
	var after string
	if len(p.Key) == 1 {
		after = p.Key[0]
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, code, name, charge_kind, amount::text, currency, status
		  FROM consumption_option
		 WHERE tenant_id = $1 AND consumption_type_id = $2::uuid
		   AND ($3 = '' OR code > $3)
		 ORDER BY code
		 LIMIT $4
	`, tenantID, typeID, after, p.Fetch())
	if err != nil {
		return nil, fmt.Errorf("consumption: list options: %w", err)
	}
	defer rows.Close()

	var out []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Code, &o.Name, &o.ChargeKind,
			&o.Amount, &o.Currency, &o.Status); err != nil {
			return nil, fmt.Errorf("consumption: scan option: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// OptionKey is the sort key ListOptions orders by.
func OptionKey(o Option) []string { return []string{o.Code} }
