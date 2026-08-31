//go:build !dev

package auth_test

import (
	"strings"
	"testing"

	"github.com/nandgator/ymca/backend/internal/auth"
)

// This is the ADR-106 guarantee (D2), tested: a build without -tags dev
// carries internal/auth/dev nowhere, so its init() never ran and "dev" is
// simply never in the registry.
func TestOpen_NoDevProviderWithoutBuildTag(t *testing.T) {
	_, err := auth.Open("dev", auth.Deps{})
	if err == nil {
		t.Fatal("Open(\"dev\") succeeded without -tags dev")
	}
	if !strings.Contains(err.Error(), "no dev provider") {
		t.Fatalf("error does not say the build has no dev provider: %v", err)
	}
}

func TestOpen_UnknownProviderWithoutBuildTag(t *testing.T) {
	if _, err := auth.Open("cognito", auth.Deps{}); err == nil {
		t.Fatal("Open(\"cognito\") succeeded")
	}
}
