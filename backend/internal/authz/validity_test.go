package authz

import (
	"context"
	"testing"
)

// The three unbuilt limbs of step 3 (D5) must stay named, visible no-ops —
// not silently absent — so this pins that each reports Valid without ever
// touching its pgx.Tx argument (passed as nil here; a limb that dereferenced
// it would panic instead of passing). No container needed: that is the
// whole point of these limbs not being implemented yet.
func TestUnimplementedValidityLimbsReportValid(t *testing.T) {
	ctx := context.Background()

	if result, err := checkTermWindow(ctx, nil, "some-principal-id"); err != nil || !result.Valid {
		t.Fatalf("checkTermWindow = %+v, %v; want {Valid: true}, nil", result, err)
	}
	if result, err := checkClearance(ctx, nil, "some-principal-id"); err != nil || !result.Valid {
		t.Fatalf("checkClearance = %+v, %v; want {Valid: true}, nil", result, err)
	}
	if result, err := checkRestriction(ctx, nil, "some-principal-id"); err != nil || !result.Valid {
		t.Fatalf("checkRestriction = %+v, %v; want {Valid: true}, nil", result, err)
	}
}
