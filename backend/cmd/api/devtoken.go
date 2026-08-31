//go:build dev

// devtoken.go adds the mint-token subcommand to cmd/api, for minting a dev
// JWT without a running IdP. //go:build dev, matching D2: a build intended
// for deployment does not carry this file, and therefore does not carry
// the ability to mint a credential either. Importing internal/auth/dev
// here is also what makes the dev provider available to auth.Open in a
// -tags dev build of this binary at all — see internal/auth/dev's init.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nandgator/ymca/backend/internal/auth/dev"
)

func init() {
	commands["mint-token"] = mintTokenCommand
}

// mintTokenCommand implements `api mint-token <sub> <tenant>`. The minted
// token is valid from now for 24 hours — long enough for a development
// session, short enough that a leaked one is not a standing credential.
func mintTokenCommand(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: api mint-token <sub> <tenant>")
	}
	sub, tenant := args[0], args[1]

	secret := os.Getenv("YMCA_DEV_AUTH_SECRET")
	if secret == "" {
		return fmt.Errorf("YMCA_DEV_AUTH_SECRET is not set")
	}

	now := time.Now()
	token, err := dev.Mint([]byte(secret), sub, tenant, now, now.Add(24*time.Hour))
	if err != nil {
		return err
	}

	fmt.Println(token)
	return nil
}
