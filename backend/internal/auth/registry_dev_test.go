//go:build dev

package auth_test

import (
	"testing"

	"github.com/nandgator/ymca/backend/internal/auth"
	_ "github.com/nandgator/ymca/backend/internal/auth/dev" // registers "dev" via init
)

// This is the other half of the ADR-106 guarantee (D2): built WITH -tags
// dev, "dev" is registered, but there is still no default — Open("") and
// Open("cognito") must still fail, and only the exact name "dev" works.
func TestOpen_EmptyNameFailsWithBuildTag(t *testing.T) {
	if _, err := auth.Open("", auth.Deps{}); err == nil {
		t.Fatal(`Open("") succeeded`)
	}
}

func TestOpen_UnknownProviderFailsWithBuildTag(t *testing.T) {
	if _, err := auth.Open("cognito", auth.Deps{}); err == nil {
		t.Fatal(`Open("cognito") succeeded`)
	}
}

// Proves "dev" IS registered under -tags dev, without needing a live
// database: Open("dev") reaches the dev package's constructor (which then
// fails for an unrelated reason — a short secret), rather than failing at
// the registry lookup with "unknown provider".
func TestOpen_DevProviderRegisteredWithBuildTag(t *testing.T) {
	_, err := auth.Open("dev", auth.Deps{DevAuthSecret: "too-short"})
	if err == nil {
		t.Fatal(`Open("dev") with a short secret succeeded`)
	}
	if err.Error() == `auth: unknown provider "dev"` {
		t.Fatalf("dev provider not registered: %v", err)
	}
}
