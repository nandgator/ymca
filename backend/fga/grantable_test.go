package fga_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nandgator/ymca/backend/fga"
	"github.com/nandgator/ymca/backend/migrations"
)

// A1.8 rule 10. ADR-110 says a permission is role-grantable if and only if
// `role_assignment#holder` is in its type restriction in A1.2. The migrations
// repeat that set as `grantable_permission` rows, so that role_permission can
// reference it with a foreign key.
//
// Two statements of one set is a drift waiting to happen, and the drift is
// silent in the dangerous direction: a permission added to the seed but not to
// the model becomes grantable in PostgreSQL and unresolvable in OpenFGA, so a
// role confers something that never works and no error says so. This test is
// what stops the two separating.
func TestGrantableSetMatchesMigration(t *testing.T) {
	fromModel := grantableFromModel(t)
	fromSeed := grantableFromMigration(t)

	if len(fromModel) == 0 {
		t.Fatal("parsed no grantable permissions out of model.fga; " +
			"the type-restriction spelling has changed")
	}

	for _, p := range difference(fromModel, fromSeed) {
		t.Errorf("%s names role_assignment#holder in A1.2 but is not seeded "+
			"into grantable_permission by any migration", p)
	}
	for _, p := range difference(fromSeed, fromModel) {
		t.Errorf("%s is seeded into grantable_permission but does not name "+
			"role_assignment#holder in A1.2 — a role could be given a "+
			"permission the graph will never resolve", p)
	}
	t.Logf("%d grantable permissions, model and migration agree", len(fromModel))
}

var (
	typeLine   = regexp.MustCompile(`^type\s+(\w+)\s*$`)
	defineLine = regexp.MustCompile(`^define\s+(\w+)\s*:`)
	seedValue  = regexp.MustCompile(`^\('([a-z_]+\.[a-z_]+)'\)[,;]$`)
)

// grantableFromModel walks the DSL tracking the enclosing type, and collects
// "<type>.<relation>" for every define whose type restriction names the
// role_assignment userset.
func grantableFromModel(t *testing.T) []string {
	t.Helper()

	var (
		out     []string
		current string
	)
	for _, raw := range strings.Split(fga.ModelDSL(), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if m := typeLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		m := defineLine.FindStringSubmatch(line)
		if m == nil || !strings.Contains(line, "role_assignment#holder") {
			continue
		}
		if current == "" {
			t.Fatalf("define outside any type: %q", line)
		}
		out = append(out, current+"."+m[1])
	}
	sort.Strings(out)
	return out
}

// grantableFromMigration reads every INSERT that seeds grantable_permission,
// across all migrations, and returns their union.
//
// It deliberately reads the migrations rather than the live database: this
// test must fail on a checkout, before anything has been applied anywhere.
//
// It reads ALL of them, not migration 0002 alone, and that is not tidiness.
// The first version looked only at 0002 because 0002 was the only migration
// that seeded anything. The moment a second one did — 0004, adding
// tenant.may_register_person — the guard failed claiming the permission was
// "not seeded", which was false, and named 0002 as the file to fix. Under
// forward-only migrations (07.4) 0002 has already been applied and must never
// be edited, so the message pointed squarely at the one repair that would
// corrupt the schema. A guard whose failure message sends you the wrong way
// is worse than no guard.
//
// Known limitation, stated rather than hidden: this unions INSERTs and does
// not interpret a DELETE. A migration that retires a permission by deleting
// its row would leave this test still expecting it. Nothing does that yet;
// when something does, this function has to grow a notion of order.
func grantableFromMigration(t *testing.T) []string {
	t.Helper()

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	const marker = "INSERT INTO grantable_permission (permission) VALUES"

	seen := make(map[string]bool)
	var out []string
	files := 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read migration %s: %v", e.Name(), err)
		}
		body := string(b)

		// A single migration may seed more than once; take every block.
		for rest := body; ; {
			i := strings.Index(rest, marker)
			if i < 0 {
				break
			}
			files++
			rest = rest[i+len(marker):]
			for _, raw := range strings.Split(rest, "\n") {
				line := strings.TrimSpace(raw)
				if line == "" {
					continue
				}
				m := seedValue.FindStringSubmatch(line)
				if m == nil {
					break // end of the VALUES list
				}
				if !seen[m[1]] {
					seen[m[1]] = true
					out = append(out, m[1])
				}
			}
		}
	}

	if files == 0 {
		t.Fatalf("no migration contains %q — the seeding spelling has changed, "+
			"and this test would otherwise report every grantable permission "+
			"as missing", marker)
	}

	sort.Strings(out)
	return out
}

func difference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}
