// fgawriter.go is the dispatcher's OpenFGA seam. It lives here rather than in
// internal/authz because the dependency runs one way: authz decides, outbox
// projects, and neither should need the other's types to compile.
package outbox

import (
	"context"
	"fmt"

	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/config"
)

// FGAWriter writes tuples to OpenFGA on the dispatcher's behalf.
type FGAWriter struct {
	client *client.OpenFgaClient
}

// NewFGAWriter opens a client pinned to the same store and model the check
// path uses. Pinning matters here for the reason 7.4 gives: the model is
// deployed ahead of the code that depends on it, so a renderer producing a
// relation the pinned model lacks fails loudly at dispatch rather than
// writing a tuple no check will ever resolve.
func NewFGAWriter(cfg config.Config) (*FGAWriter, error) {
	c, err := client.NewSdkClient(&client.ClientConfiguration{
		ApiUrl:               cfg.FGAAPIURL,
		StoreId:              cfg.FGAStoreID,
		AuthorizationModelId: cfg.FGAModelID,
	})
	if err != nil {
		return nil, fmt.Errorf("outbox: open openfga client: %w", err)
	}
	return &FGAWriter{client: c}, nil
}

// WriteTuples writes every tuple for one fact.
//
// Tuple writes are idempotent by nature (8.9), but OpenFGA rejects a write
// whose tuple already exists. A redelivered row must therefore not fail on
// that, or at-least-once delivery would turn every retry into a permanent
// error. Existing tuples are filtered out first, and a write of nothing is a
// success rather than a call.
func (w *FGAWriter) WriteTuples(ctx context.Context, tuples []Tuple) error {
	pending := make([]client.ClientTupleKey, 0, len(tuples))
	for _, t := range tuples {
		exists, err := w.exists(ctx, t)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		pending = append(pending, client.ClientTupleKey{
			User: t.User, Relation: t.Relation, Object: t.Object,
		})
	}
	if len(pending) == 0 {
		return nil
	}

	if _, err := w.client.Write(ctx).Body(client.ClientWriteRequest{
		Writes: pending,
	}).Execute(); err != nil {
		return fmt.Errorf("outbox: write %d tuples: %w", len(pending), err)
	}
	return nil
}

// exists reports whether the tuple is already present, so redelivery is a
// no-op rather than an error.
func (w *FGAWriter) exists(ctx context.Context, t Tuple) (bool, error) {
	resp, err := w.client.Read(ctx).Body(client.ClientReadRequest{
		User:     &t.User,
		Relation: &t.Relation,
		Object:   &t.Object,
	}).Execute()
	if err != nil {
		return false, fmt.Errorf("outbox: read tuple %s %s %s: %w",
			t.User, t.Relation, t.Object, err)
	}
	return len(resp.GetTuples()) > 0, nil
}

// DeleteTuples removes tuples synchronously, for the revocation path of 8.3.
// A delete that fails must fail the revoking transaction: the failure mode is
// "revocation did not happen and you were told", never "revocation appeared
// to succeed but authority persisted".
func (w *FGAWriter) DeleteTuples(ctx context.Context, tuples []Tuple) error {
	if len(tuples) == 0 {
		return nil
	}
	deletes := make([]client.ClientTupleKeyWithoutCondition, 0, len(tuples))
	for _, t := range tuples {
		exists, err := w.exists(ctx, t)
		if err != nil {
			return err
		}
		if !exists {
			// Already absent. The revocation's goal is that the tuple not
			// exist, and it does not.
			continue
		}
		deletes = append(deletes, client.ClientTupleKeyWithoutCondition{
			User: t.User, Relation: t.Relation, Object: t.Object,
		})
	}
	if len(deletes) == 0 {
		return nil
	}
	if _, err := w.client.Write(ctx).Body(client.ClientWriteRequest{
		Deletes: deletes,
	}).Execute(); err != nil {
		return fmt.Errorf("outbox: delete %d tuples: %w", len(deletes), err)
	}
	return nil
}
