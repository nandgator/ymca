// Command fga applies backend/fga/model.fga to an OpenFGA server and runs the
// assertions in backend/fga/assertions.yaml against it.
//
//	fga apply    write the model to the store named by YMCA_FGA_STORE_ID,
//	             creating the store if that variable is empty; print the
//	             store and model ids
//	fga test     create a throwaway store, apply the model, write the
//	             fixture, run every assertion, delete the store
//
// `fga test` is the CI command. A1.8 rule 4 requires that every model change
// add assertions; this is what makes that requirement enforceable rather than
// aspirational. A single failed assertion exits non-zero.
//
// The model and the assertions are embedded, so the binary carries its own
// copy of both and cannot be run against a stale checkout.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openfga/go-sdk/client"
	"github.com/openfga/language/pkg/go/transformer"

	fgafiles "github.com/nandgator/ymca/backend/fga"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fga: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: fga <apply|test>")
	}

	apiURL := os.Getenv("YMCA_FGA_API_URL")
	if apiURL == "" {
		return errors.New("YMCA_FGA_API_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch os.Args[1] {
	case "apply":
		return apply(ctx, apiURL)
	case "test":
		return test(ctx, apiURL)
	default:
		return fmt.Errorf("unknown command %q: expected apply or test", os.Args[1])
	}
}

// apply writes the model to a durable store. 7.4 requires the model to be
// deployed before the code that depends on it, so this runs ahead of the API.
func apply(ctx context.Context, apiURL string) error {
	storeID := os.Getenv("YMCA_FGA_STORE_ID")

	fga, err := client.NewSdkClient(&client.ClientConfiguration{ApiUrl: apiURL})
	if err != nil {
		return fmt.Errorf("open fga client: %w", err)
	}

	if storeID == "" {
		created, err := fga.CreateStore(ctx).
			Body(client.ClientCreateStoreRequest{Name: "ymca"}).
			Execute()
		if err != nil {
			return fmt.Errorf("create store: %w", err)
		}
		storeID = created.Id
		fmt.Printf("created store %s\n", storeID)
	}
	if err := fga.SetStoreId(storeID); err != nil {
		return fmt.Errorf("set store: %w", err)
	}

	modelID, err := writeModel(ctx, fga)
	if err != nil {
		return err
	}

	fmt.Printf("store %s\nmodel %s\n", storeID, modelID)
	fmt.Printf("\nexport YMCA_FGA_STORE_ID=%s\nexport YMCA_FGA_MODEL_ID=%s\n", storeID, modelID)
	return nil
}

// test proves the model in isolation. A throwaway store means the assertions
// never see tuples that some other run left behind — a fixture that leaks
// between runs turns a DENY assertion into a coin flip.
func test(ctx context.Context, apiURL string) error {
	fga, err := client.NewSdkClient(&client.ClientConfiguration{ApiUrl: apiURL})
	if err != nil {
		return fmt.Errorf("open fga client: %w", err)
	}

	created, err := fga.CreateStore(ctx).
		Body(client.ClientCreateStoreRequest{
			Name: fmt.Sprintf("ymca-assertions-%d", time.Now().UnixNano()),
		}).Execute()
	if err != nil {
		return fmt.Errorf("create throwaway store: %w", err)
	}
	storeID := created.Id
	if err := fga.SetStoreId(storeID); err != nil {
		return fmt.Errorf("set store: %w", err)
	}
	defer func() {
		if _, err := fga.DeleteStore(context.WithoutCancel(ctx)).Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not delete throwaway store %s: %v\n", storeID, err)
		}
	}()

	modelID, err := writeModel(ctx, fga)
	if err != nil {
		return err
	}

	suite, err := fgafiles.LoadAssertions()
	if err != nil {
		return err
	}

	if err := writeTuples(ctx, fga, suite.Tuples); err != nil {
		return err
	}

	// The role path is never written. It is supplied on every Check exactly
	// as internal/authz supplies it at runtime, from PostgreSQL, once the
	// term window and clearance have been resolved (ADR-109). Writing these
	// instead would prove the model resolves a shape the system never
	// produces.
	contextual := make([]client.ClientContextualTupleKey, 0, len(suite.Contextual))
	for _, t := range suite.Contextual {
		contextual = append(contextual, client.ClientContextualTupleKey{
			User:     t.User,
			Relation: t.Relation,
			Object:   t.Object,
		})
	}

	fmt.Printf("store %s (throwaway)\nmodel %s\n%d tuples, %d contextual, %d assertions\n\n",
		storeID, modelID, len(suite.Tuples), len(contextual), len(suite.Assertions))

	var failures []string
	for _, a := range suite.Assertions {
		got, err := fga.Check(ctx).Body(client.ClientCheckRequest{
			User:             a.User,
			Relation:         a.Relation,
			Object:           a.Object,
			ContextualTuples: contextual,
		}).Execute()
		if err != nil {
			// An error is not a DENY. A model that rejects the check as
			// invalid has not proved the assertion either way, and reporting
			// it as a pass would be the exact failure this suite exists to
			// prevent.
			failures = append(failures, fmt.Sprintf(
				"ERROR  %s\n         check(%s, %s, %s) failed: %v",
				a.Name, a.User, a.Relation, a.Object, err))
			fmt.Printf("ERROR  %s\n", a.Name)
			continue
		}
		allowed := got.GetAllowed()
		if allowed != a.Expect {
			failures = append(failures, fmt.Sprintf(
				"FAIL   %s\n         check(%s, %s, %s) = %v, want %v",
				a.Name, a.User, a.Relation, a.Object, allowed, a.Expect))
			fmt.Printf("FAIL   %s\n", a.Name)
			continue
		}
		verdict := "DENY "
		if allowed {
			verdict = "ALLOW"
		}
		fmt.Printf("ok     %s  [%s]\n", a.Name, verdict)
	}

	failures = append(failures, checkForbidden(ctx, fga, suite.Forbidden)...)

	fmt.Println()
	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n", strings.Join(failures, "\n"))
		return fmt.Errorf("%d of %d assertions failed", len(failures), len(suite.Assertions))
	}
	fmt.Printf("all %d assertions passed\n", len(suite.Assertions))
	return nil
}

// checkForbidden proves the grantable set of ADR-110 by trying to write each
// forbidden tuple and requiring the store to refuse it. A write that succeeds
// means a relation became role-grantable — the privilege-escalation route of
// ADR-078.
//
// It runs after the assertions, so an accepted tuple cannot alter a verdict
// already reached. The delete is for the case where this suite is ever
// pointed at a store that outlives the run.
func checkForbidden(ctx context.Context, fga *client.OpenFgaClient, forbidden []fgafiles.Tuple) []string {
	var failures []string
	for _, t := range forbidden {
		key := client.ClientTupleKey{User: t.User, Relation: t.Relation, Object: t.Object}
		_, err := fga.Write(ctx).Body(client.ClientWriteRequest{
			Writes: []client.ClientTupleKey{key},
		}).Execute()
		if err != nil {
			fmt.Printf("ok     refused  %s %s %s\n", t.User, t.Relation, t.Object)
			continue
		}
		failures = append(failures, fmt.Sprintf(
			"FAIL   forbidden tuple was ACCEPTED: %s %s %s\n"+
				"         %s is role-grantable, and ADR-110 says it must not be.\n"+
				"         Remove role_assignment#holder from its type restriction in A1.2.",
			t.User, t.Relation, t.Object, t.Relation))
		fmt.Printf("FAIL   accepted  %s %s %s\n", t.User, t.Relation, t.Object)

		if _, err := fga.Write(ctx).Body(client.ClientWriteRequest{
			Deletes: []client.ClientTupleKeyWithoutCondition{{
				User: t.User, Relation: t.Relation, Object: t.Object,
			}},
		}).Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove forbidden tuple: %v\n", err)
		}
	}
	return failures
}

func writeModel(ctx context.Context, fga *client.OpenFgaClient) (string, error) {
	modelJSON, err := transformer.TransformDSLToJSON(fgafiles.ModelDSL())
	if err != nil {
		return "", fmt.Errorf("parse fga/model.fga: %w", err)
	}

	var body client.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &body); err != nil {
		return "", fmt.Errorf("decode transformed model: %w", err)
	}

	written, err := fga.WriteAuthorizationModel(ctx).Body(body).Execute()
	if err != nil {
		return "", fmt.Errorf("write authorization model: %w", err)
	}
	modelID := written.GetAuthorizationModelId()
	if err := fga.SetAuthorizationModelId(modelID); err != nil {
		return "", fmt.Errorf("pin model: %w", err)
	}
	return modelID, nil
}

// writeTuples sends the fixture in batches. OpenFGA caps the number of tuples
// per Write call; the cap is smaller than this fixture.
func writeTuples(ctx context.Context, fga *client.OpenFgaClient, tuples []fgafiles.Tuple) error {
	const batch = 20
	for start := 0; start < len(tuples); start += batch {
		end := min(start+batch, len(tuples))

		writes := make([]client.ClientTupleKey, 0, end-start)
		for _, t := range tuples[start:end] {
			writes = append(writes, client.ClientTupleKey{
				User:     t.User,
				Relation: t.Relation,
				Object:   t.Object,
			})
		}
		if _, err := fga.Write(ctx).Body(client.ClientWriteRequest{Writes: writes}).Execute(); err != nil {
			return fmt.Errorf("write fixture tuples %d-%d: %w", start, end-1, err)
		}
	}
	return nil
}
