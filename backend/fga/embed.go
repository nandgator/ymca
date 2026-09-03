// Package fga carries the OpenFGA authorization model and its assertion
// suite as embedded files, and parses the latter.
//
// model.fga is A1.2 verbatim and assertions.yaml is A1.6 plus A1.7 made
// executable. A1.8 governs changes to both: every new relation names the ADR
// it realizes, every new permission declares its propagation, and every
// change adds assertions — including negative ones.
//
// The two files and the design document must not drift. If A1.2 changes,
// re-extract; if this model changes, A1.2 is wrong.
package fga

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed model.fga
var modelDSL string

//go:embed assertions.yaml
var assertionsYAML []byte

// ModelDSL returns the authorization model in the OpenFGA DSL.
func ModelDSL() string { return modelDSL }

// Tuple is one relationship in the assertion fixture: user has relation on
// object.
type Tuple struct {
	User     string `yaml:"user"`
	Relation string `yaml:"relation"`
	Object   string `yaml:"object"`
}

// Assertion is one check with its expected outcome. Expect is spelled out
// rather than defaulted, so that a mistyped key produces a false expectation
// that fails loudly instead of a silent DENY assertion that always passes.
type Assertion struct {
	Name     string `yaml:"name"`
	User     string `yaml:"user"`
	Relation string `yaml:"relation"`
	Object   string `yaml:"object"`
	Expect   bool   `yaml:"expect"`
}

// Suite is the whole of assertions.yaml.
type Suite struct {
	Tuples []Tuple `yaml:"tuples"`
	// Contextual holds the role path (A1.6). These are NOT written to the
	// store: they are supplied on every Check as contextual tuples, which
	// is how the running system supplies them too (ADR-109). The suite is
	// therefore exercising the real mechanism rather than a lookalike that
	// happens to resolve.
	Contextual []Tuple `yaml:"contextual"`
	// Forbidden holds tuples the model must REFUSE outright. They encode
	// the grantable set of ADR-110: a relation is role-grantable exactly
	// when `role_assignment#holder` is in its type restriction, so a role
	// tuple against any other relation is a type violation and OpenFGA
	// rejects it. Asserting a DENY here would be weaker — a DENY means the
	// tuple was accepted and merely failed to resolve.
	Forbidden  []Tuple     `yaml:"forbidden"`
	Assertions []Assertion `yaml:"assertions"`
}

// roleAssignmentPrefix is the type whose tuples may never be persisted.
const roleAssignmentPrefix = "role_assignment:"

// storable reports whether a tuple may be written to the store. A role
// assignment's effectiveness — term window, clearance, ACTING cover — is a
// PostgreSQL fact resolved per check (ADR-109). A stored role tuple would
// outlive the term that justified it, which is exactly the sweeper
// dependency ADR-070 refuses. Enforced here rather than asked for in a
// comment, because A1.8's six prose rules were all obeyed while three
// defects shipped.
func storable(t Tuple) bool {
	return !strings.HasPrefix(t.Object, roleAssignmentPrefix) &&
		!strings.HasPrefix(t.User, roleAssignmentPrefix)
}

// LoadAssertions parses the embedded suite.
func LoadAssertions() (Suite, error) {
	var s Suite
	dec := yaml.NewDecoder(bytes.NewReader(assertionsYAML))
	dec.KnownFields(true) // a typo in a key is a defect, not a default
	if err := dec.Decode(&s); err != nil {
		return Suite{}, fmt.Errorf("parse fga/assertions.yaml: %w", err)
	}

	if len(s.Assertions) == 0 {
		return Suite{}, fmt.Errorf("fga/assertions.yaml declares no assertions")
	}
	for i, a := range s.Assertions {
		if a.Name == "" || a.User == "" || a.Relation == "" || a.Object == "" {
			return Suite{}, fmt.Errorf("assertion %d is incomplete: %+v", i, a)
		}
	}
	for i, t := range s.Tuples {
		if t.User == "" || t.Relation == "" || t.Object == "" {
			return Suite{}, fmt.Errorf("tuple %d is incomplete: %+v", i, t)
		}
		if !storable(t) {
			return Suite{}, fmt.Errorf(
				"tuple %d writes a role assignment to the store: %s %s %s.\n"+
					"Role assignments are resolved per check and supplied as contextual\n"+
					"tuples (ADR-109) — move this line to the `contextual:` block",
				i, t.User, t.Relation, t.Object)
		}
	}
	for i, t := range s.Contextual {
		if t.User == "" || t.Relation == "" || t.Object == "" {
			return Suite{}, fmt.Errorf("contextual tuple %d is incomplete: %+v", i, t)
		}
	}
	if len(s.Forbidden) == 0 {
		return Suite{}, fmt.Errorf(
			"fga/assertions.yaml declares no forbidden tuples; the grantable set " +
				"of ADR-110 would then rest on nothing")
	}
	for i, t := range s.Forbidden {
		if t.User == "" || t.Relation == "" || t.Object == "" {
			return Suite{}, fmt.Errorf("forbidden tuple %d is incomplete: %+v", i, t)
		}
		// This block means one thing only: the relation is outside the
		// grantable set. A tuple with some other subject would be refused
		// for an unrelated reason and still report as proof of ADR-110,
		// which is the vacuous-negative-test failure A1.7 already guards
		// against elsewhere.
		if !strings.HasPrefix(t.User, roleAssignmentPrefix) {
			return Suite{}, fmt.Errorf(
				"forbidden tuple %d has subject %q, not a role assignment: %s %s %s.\n"+
					"The forbidden block proves the grantable set of ADR-110; a tuple\n"+
					"refused for any other reason proves nothing about it",
				i, t.User, t.User, t.Relation, t.Object)
		}
	}
	return s, nil
}
