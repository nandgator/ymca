//go:build integration

package organization_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nandgator/ymca/backend/internal/db"
	"github.com/nandgator/ymca/backend/internal/organization"
)

// The claim under test: ADR-016's invariants hold against a writer that does
// NOT go through CreateUnit. That is the whole reason migration 0005 puts
// them in a trigger (ADR-115) — a cycle is unreachable through
// POST /t/{t}/units by construction, so a guard living in that endpoint
// could never be seen to fail.
//
// Every case below writes the edge directly. If these tests only exercised
// CreateUnit, the cycle and depth cases would be testing the endpoint's
// arithmetic rather than the database's invariant, and removing the trigger
// would leave them green.

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	url := os.Getenv("YMCA_DATABASE_URL")
	if url == "" {
		t.Fatal("YMCA_DATABASE_URL must be set to run integration tests (role ymca_api)")
	}
	d, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

const (
	edgeTenant  = "77777777-0000-0000-0000-000000000001"
	otherTenant = "77777777-0000-0000-0000-000000000002"
	actor       = "77777777-0000-0000-0000-00000000000f"
)

// seedEdgeFixture creates two tenants and returns a helper that makes units.
// It cleans up in reverse dependency order, and the outbox rows go too:
// CreateUnit enqueues facts, and a pending row left behind is counted by the
// NEXT run of some other package's test — the failure session 5 found.
func seedEdgeFixture(t *testing.T, d *db.DB) func(name string) string {
	t.Helper()
	ctx := context.Background()

	if _, err := d.Pool().Exec(ctx, `
		INSERT INTO tenant (id, legal_name, display_name, jurisdiction, status)
		VALUES ($1,'Edge Test','Edge Test','IN','ACTIVE'),
		       ($2,'Edge Other','Edge Other','IN','ACTIVE')
		ON CONFLICT (id) DO NOTHING
	`, edgeTenant, otherTenant); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		for _, tenant := range []string{edgeTenant, otherTenant} {
			// The unit ids must be collected BEFORE the rows go, because the
			// outbox is what they are needed for and it carries no tenant_id
			// to find them by afterwards.
			var unitIDs []string
			_ = d.InTenantTx(bg, tenant, func(tx pgx.Tx) error {
				rows, err := tx.Query(bg,
					`SELECT id FROM organizational_unit WHERE tenant_id = $1`, tenant)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var id string
					if err := rows.Scan(&id); err != nil {
						return err
					}
					unitIDs = append(unitIDs, id)
				}
				return rows.Err()
			})
			_ = d.InTenantTx(bg, tenant, func(tx pgx.Tx) error {
				_, _ = tx.Exec(bg, `DELETE FROM authorization_edge WHERE tenant_id = $1`, tenant)
				_, _ = tx.Exec(bg, `DELETE FROM organizational_unit WHERE tenant_id = $1`, tenant)
				return nil
			})
			// By aggregate_id, never by payload->>'tenant_id': the
			// AuthorizationEdgeCreated payload has no tenant_id, so scoping
			// that way leaves every edge fact behind. This test wrote 61 such
			// rows before the mistake was caught -- the same one the
			// configuration test's cleanup comment already warns about, made
			// a second time.
			if len(unitIDs) > 0 {
				_, _ = d.Pool().Exec(bg,
					`DELETE FROM authorization_outbox WHERE aggregate_id = ANY($1::uuid[])`, unitIDs)
			}
		}
		_, _ = d.Pool().Exec(bg, `DELETE FROM tenant WHERE id = ANY($1)`,
			[]string{edgeTenant, otherTenant})
	})

	return func(name string) string {
		t.Helper()
		var id string
		if err := d.InTenantTx(ctx, edgeTenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO organizational_unit (id, tenant_id, type, name, status)
				VALUES (gen_random_uuid(), $1, 'CHAPTER', $2, 'ACTIVE')
				RETURNING id::text`, edgeTenant, name).Scan(&id)
		}); err != nil {
			t.Fatalf("create unit %s: %v", name, err)
		}
		return id
	}
}

// insertEdge writes an edge the way nothing in the application does: no
// CreateUnit, no validation, straight at the table.
func insertEdge(t *testing.T, d *db.DB, childType, childID, parentType, parentID string) error {
	t.Helper()
	ctx := context.Background()
	return d.InTenantTx(ctx, edgeTenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO authorization_edge
			    (id, tenant_id, child_type, child_id, parent_type, parent_id, created_by)
			VALUES (gen_random_uuid(), $1, $2, $3::uuid, $4, $5::uuid, $6::uuid)
		`, edgeTenant, childType, childID, parentType, parentID, actor)
		return err
	})
}

func TestAuthorizationEdge_RefusesACycle(t *testing.T) {
	d := openTestDB(t)
	unit := seedEdgeFixture(t, d)

	a, b, c := unit("cycle A"), unit("cycle B"), unit("cycle C")

	// A -> tenant, B -> A, C -> B. A legal chain three deep.
	for _, e := range [][4]string{
		{"organizational_unit", a, "tenant", edgeTenant},
		{"organizational_unit", b, "organizational_unit", a},
		{"organizational_unit", c, "organizational_unit", b},
	} {
		if err := insertEdge(t, d, e[0], e[1], e[2], e[3]); err != nil {
			t.Fatalf("legal edge %s -> %s: %v", e[1], e[3], err)
		}
	}

	// Now close the loop: A under C. Nothing about this row is malformed —
	// it violates only the shape of the graph it would join.
	err := insertEdge(t, d, "organizational_unit", a, "organizational_unit", c)
	if err == nil {
		t.Fatal("a cycle A -> C -> B -> A was accepted; the DAG is a graph " +
			"and ADR-016's guarantee does not hold")
	}

	// The self-edge is a separate branch of the trigger, and a one-node
	// cycle is exactly the case a walk seeded at the parent can miss.
	if err := insertEdge(t, d, "organizational_unit", a, "organizational_unit", a); err == nil {
		t.Fatal("a unit was accepted as its own authorization parent")
	}
}

func TestAuthorizationEdge_RefusesPastDepthTwelve(t *testing.T) {
	d := openTestDB(t)
	unit := seedEdgeFixture(t, d)

	// The root's edge to the tenant is edge 1, so twelve units make twelve
	// edges and the thirteenth is one too many. Asserting the LIMIT holds is
	// not enough on its own: a trigger that refused everything would pass
	// that half, so the twelfth must be accepted as well.
	parentType, parentID := "tenant", edgeTenant
	for i := 1; i <= 12; i++ {
		child := unit("depth")
		if err := insertEdge(t, d, "organizational_unit", child, parentType, parentID); err != nil {
			t.Fatalf("edge %d of 12 was refused, and 12 is legal: %v", i, err)
		}
		parentType, parentID = "organizational_unit", child
	}

	thirteenth := unit("depth 13")
	if err := insertEdge(t, d, "organizational_unit", thirteenth, parentType, parentID); err == nil {
		t.Fatal("a thirteenth edge was accepted; A2.2 bounds the path at 12")
	}
}

func TestAuthorizationEdge_RefusesEndpointsOutsideTheTenant(t *testing.T) {
	d := openTestDB(t)
	unit := seedEdgeFixture(t, d)
	child := unit("orphan")

	// A parent unit that does not exist. Under RLS this is also what
	// ANOTHER tenant's unit looks like from here, which is the point.
	if err := insertEdge(t, d, "organizational_unit", child,
		"organizational_unit", "77777777-0000-0000-0000-0000000000ff"); err == nil {
		t.Fatal("an edge to a parent unit that does not resolve in this tenant was accepted")
	}

	// A parent tenant that is not this tenant. The edge row's own tenant_id
	// is forced to the session tenant by RLS; this checks the thing RLS does
	// NOT check, which is what the endpoints NAME.
	if err := insertEdge(t, d, "organizational_unit", child, "tenant", otherTenant); err == nil {
		t.Fatal("an edge naming another tenant as parent was accepted")
	}

	// An edge type the trigger cannot validate. A1.2 declares auth_parent on
	// resource, programme and consumption_type; refusing them is
	// fail-closed, and the migration that makes one writable must extend
	// the trigger rather than inherit an unchecked edge.
	if err := insertEdge(t, d, "resource", child,
		"organizational_unit", child); err == nil {
		t.Fatal("an edge on an unvalidated type was accepted")
	}
}

// The mapping is a contract between a SQL constraint name and a Go error,
// and nothing else in the build would notice it breaking: renaming a
// constraint in a future migration compiles, passes vet, and silently turns
// every refusal into a 500.
func TestCreateUnit_MapsTheTriggerToDomainErrors(t *testing.T) {
	d := openTestDB(t)
	unit := seedEdgeFixture(t, d)
	ctx := context.Background()

	// Twelve edges deep, the legal maximum, built through the domain
	// function this time — so the depth case covers CreateUnit's own path
	// as well as the trigger's.
	parent := organization.Parent{Type: "tenant", ID: edgeTenant}
	for i := 1; i <= 12; i++ {
		var created organization.Unit
		if err := d.InTenantTx(ctx, edgeTenant, func(tx pgx.Tx) error {
			var err error
			created, err = organization.CreateUnit(ctx, tx, edgeTenant, organization.NewUnit{
				Type: "CHAPTER", Name: "mapped", Parent: parent, CreatedBy: actor,
			})
			return err
		}); err != nil {
			t.Fatalf("CreateUnit at depth %d: %v", i, err)
		}
		parent = organization.Parent{Type: "organizational_unit", ID: created.ID}
	}

	err := d.InTenantTx(ctx, edgeTenant, func(tx pgx.Tx) error {
		_, err := organization.CreateUnit(ctx, tx, edgeTenant, organization.NewUnit{
			Type: "CHAPTER", Name: "too deep", Parent: parent, CreatedBy: actor,
		})
		return err
	})
	if !errors.Is(err, organization.ErrEdgeTooDeep) {
		t.Fatalf("CreateUnit past depth 12 = %v, want ErrEdgeTooDeep. A trigger "+
			"refusal that does not map is a 500 rather than a 400", err)
	}

	// A cycle cannot be produced through CreateUnit — the unit is new — so
	// the cycle arm of the mapping is reached the only way it can be, by
	// making the trigger raise it directly.
	a := unit("mapped cycle A")
	b := unit("mapped cycle B")
	if err := insertEdge(t, d, "organizational_unit", b, "organizational_unit", a); err != nil {
		t.Fatalf("legal edge: %v", err)
	}
	err = insertEdge(t, d, "organizational_unit", a, "organizational_unit", b)
	if err == nil {
		t.Fatal("cycle accepted")
	}
	// insertEdge does not run through edgeError, so this asserts the raw
	// SQLSTATE and constraint name the mapping depends on rather than the
	// mapping's own output.
	if got := constraintOf(err); got != "authorization_edge_acyclic" {
		t.Fatalf("cycle refusal did not carry constraint authorization_edge_acyclic; "+
			"organization.edgeError switches on that name and would return a 500; got %q from %v",
			got, err)
	}
}

// constraintOf returns the constraint name PostgreSQL attached to a
// check_violation, or "". It reads the error the DATABASE sent rather than
// asking the package under test what it made of it — the mapping is exactly
// what is in question.
func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		return ""
	}
	return pgErr.ConstraintName
}
