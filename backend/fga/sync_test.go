package fga

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// A1.8 rule 7. The design document is the source; these files are derived from
// it. Every previous mechanism for keeping them together was a sentence asking
// a human to remember, and three defects got through a full review anyway:
// A1.2 did not parse, A1.6 was missing two tuples without which A1.7's first
// assertion could not resolve, and a declared relation was read by nothing.
//
// Prose cannot enforce prose. These tests can.

const designDoc = "../../docs/system-design/A1_openfga_schema.md"

// TestModelMatchesA1_2 fails if backend/fga/model.fga differs from the DSL
// block in A1.2 by a single character.
func TestModelMatchesA1_2(t *testing.T) {
	doc := readDesignDoc(t)

	want, err := extractFence(doc, "## A1.2 The model", "```dsl")
	if err != nil {
		t.Fatalf("A1.2: %v", err)
	}

	if got := ModelDSL(); got != want {
		t.Errorf("fga/model.fga has drifted from A1.2 in %s.\n"+
			"A1.2 is the source: re-extract, or change A1.2 first.\n%s",
			designDoc, firstDifference(got, want))
	}
}

// TestAssertionsCoverA1_7 fails if an assertion the design document promises is
// not actually run, or is run with the opposite expectation. It does not
// require the reverse: the suite may hold extra assertions, and the fixture
// tuples are implementation.
func TestAssertionsCoverA1_7(t *testing.T) {
	doc := readDesignDoc(t)

	section, ok := sectionBetween(doc, "## A1.7 Assertions", "## A1.8")
	if !ok {
		t.Fatalf("no A1.7 section in %s", designDoc)
	}

	suite, err := LoadAssertions()
	if err != nil {
		t.Fatalf("load suite: %v", err)
	}
	run := make(map[string]bool, len(suite.Assertions))
	for _, a := range suite.Assertions {
		run[a.User+" "+a.Relation+" "+a.Object] = a.Expect
	}

	promised, err := parseA1_7(section)
	if err != nil {
		t.Fatal(err)
	}
	if len(promised) == 0 {
		t.Fatal("parsed no assertions out of A1.7; the section's shape has changed")
	}

	for _, p := range promised {
		key := p.user + " " + p.relation + " " + p.object
		got, ran := run[key]
		switch {
		case !ran:
			t.Errorf("A1.7 promises %q but nothing runs it", key)
		case got != p.expect:
			t.Errorf("A1.7 expects %s for %q; the suite expects %s",
				verdict(p.expect), key, verdict(got))
		}
	}
	t.Logf("%d assertions promised by A1.7, all run", len(promised))
}

type promisedAssertion struct {
	user, relation, object string
	expect                 bool
}

// parseA1_7 reads the ALLOW and DENY blocks. Headings set the expectation, so
// a block retitled from ALLOW to DENY flips every assertion under it and the
// suite is checked against the new meaning.
func parseA1_7(section string) ([]promisedAssertion, error) {
	var (
		out    []promisedAssertion
		expect bool
		inCode bool
		known  bool
	)
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "### ") {
			heading := strings.ToUpper(trimmed)
			switch {
			case strings.Contains(heading, "MUST ALLOW"):
				expect, known = true, true
			case strings.Contains(heading, "MUST DENY"):
				expect, known = false, true
			default:
				known = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if !inCode || !known || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) != 3 {
			return nil, fmt.Errorf("A1.7: %q is not `user relation object`", trimmed)
		}
		if !strings.Contains(fields[0], ":") {
			return nil, fmt.Errorf("A1.7: subject %q in %q carries no type; "+
				"person:x and principal:x are different subjects", fields[0], trimmed)
		}
		out = append(out, promisedAssertion{fields[0], fields[1], fields[2], expect})
	}
	return out, nil
}

func readDesignDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("read design document: %v", err)
	}
	return string(b)
}

// extractFence returns the contents of the first fenced block of the given
// kind after the given heading.
func extractFence(doc, heading, fence string) (string, error) {
	h := strings.Index(doc, heading)
	if h < 0 {
		return "", fmt.Errorf("heading %q not found", heading)
	}
	open := strings.Index(doc[h:], fence+"\n")
	if open < 0 {
		return "", fmt.Errorf("no %s block after %q", fence, heading)
	}
	open += h + len(fence) + 1

	close := strings.Index(doc[open:], "\n```")
	if close < 0 {
		return "", fmt.Errorf("unterminated %s block after %q", fence, heading)
	}
	return doc[open:open+close] + "\n", nil
}

func sectionBetween(doc, from, to string) (string, bool) {
	a := strings.Index(doc, from)
	if a < 0 {
		return "", false
	}
	b := strings.Index(doc[a:], to)
	if b < 0 {
		return doc[a:], true
	}
	return doc[a : a+b], true
}

// firstDifference reports the first differing line, which is far more use than
// a diff of two 300-line files.
func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("first difference at line %d:\n  model.fga: %q\n  A1.2:      %q", i+1, g[i], w[i])
		}
	}
	return fmt.Sprintf("model.fga has %d lines, A1.2 has %d", len(g), len(w))
}

func verdict(b bool) string {
	if b {
		return "ALLOW"
	}
	return "DENY"
}
