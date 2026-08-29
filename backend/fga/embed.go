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
	Tuples     []Tuple     `yaml:"tuples"`
	Assertions []Assertion `yaml:"assertions"`
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
	}
	return s, nil
}
