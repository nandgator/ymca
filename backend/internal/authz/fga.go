// fga.go is the authz package's OpenFGA client: one method, Check, wired
// against the model id config.go pins (D5 step 2). It exists apart from
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

// Check runs one OpenFGA check — step 2 of 6.1.
func (f *FGA) Check(ctx context.Context, user, relation, object string) (bool, error) {
	resp, err := f.client.Check(ctx).Body(client.ClientCheckRequest{
		User:     user,
		Relation: relation,
		Object:   object,
	}).Execute()
	if err != nil {
		return false, fmt.Errorf("authz: openfga check(%s, %s, %s): %w", user, relation, object, err)
	}
	return resp.GetAllowed(), nil
}
