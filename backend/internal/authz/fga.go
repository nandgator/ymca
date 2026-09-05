// fga.go is the authz package's OpenFGA client: one method, Check, wired
// against the model id config.go pins (D5, 6.1 step 3). It exists apart from
// authz.go because it is the network seam — nothing else in this package
// makes a call outside the process.
package authz

import (
	"context"
	"fmt"

	"github.com/openfga/go-sdk/client"

	"github.com/nandgator/ymca/backend/internal/config"
)

// FGA is a thin wrapper over the OpenFGA client, pinned to one store and,
// when configured, one model version.
type FGA struct {
	client *client.OpenFgaClient
}

// NewFGA opens a client against cfg.FGAAPIURL, pinned to cfg.FGAStoreID and
// cfg.FGAModelID. An empty FGAModelID checks against whichever model is
// latest — config.go's own comment on FGAModelID marks that as right for
// development and wrong for production, where 7.4 requires the model
// deployed ahead of dependent code.
func NewFGA(cfg config.Config) (*FGA, error) {
	c, err := client.NewSdkClient(&client.ClientConfiguration{ApiUrl: cfg.FGAAPIURL})
	if err != nil {
		return nil, fmt.Errorf("authz: open openfga client: %w", err)
	}
	if err := c.SetStoreId(cfg.FGAStoreID); err != nil {
		return nil, fmt.Errorf("authz: set store id: %w", err)
	}
	if err := c.SetAuthorizationModelId(cfg.FGAModelID); err != nil {
		return nil, fmt.Errorf("authz: set model id: %w", err)
	}
	return &FGA{client: c}, nil
}

// Check runs one OpenFGA check — step 3 of 6.1 — with the role path that
// step 2 resolved supplied as contextual tuples.
//
// Contextual tuples are the whole of ADR-109's mechanism at this seam: they
// are visible to this check and to nothing else, are never persisted, and
// are validated against the model, so a tuple naming a relation outside the
// grantable set is rejected here rather than quietly failing to resolve.
// That rejection is an error, not a DENY, and it surfaces as one.
func (f *FGA) Check(ctx context.Context, user, relation, object string, contextual []ContextualTuple) (bool, error) {
	body := client.ClientCheckRequest{
		User:     user,
		Relation: relation,
		Object:   object,
	}
	if len(contextual) > 0 {
		body.ContextualTuples = make([]client.ClientContextualTupleKey, 0, len(contextual))
		for _, t := range contextual {
			body.ContextualTuples = append(body.ContextualTuples, client.ClientContextualTupleKey{
				User:     t.User,
				Relation: t.Relation,
				Object:   t.Object,
			})
		}
	}

	resp, err := f.client.Check(ctx).Body(body).Execute()
	if err != nil {
		return false, fmt.Errorf("authz: openfga check(%s, %s, %s) with %d contextual tuples: %w",
			user, relation, object, len(contextual), err)
	}
	return resp.GetAllowed(), nil
}
