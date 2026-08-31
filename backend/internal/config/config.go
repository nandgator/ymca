// Package config reads process configuration from the environment.
//
// Nothing here has a default that would work in production by accident. A
// missing required value is an error at start-up, not a fallback, because a
// service that silently connects to the wrong database is worse than one that
// refuses to start.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is the whole of what the process needs to know before it can serve.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string. The role it names must
	// NOT be a superuser and must NOT hold BYPASSRLS: row level security is
	// how tenant isolation is enforced (A2.1), and both of those defeat it.
	DatabaseURL string

	// FGAAPIURL is the OpenFGA HTTP API, e.g. http://localhost:8080.
	FGAAPIURL string

	// FGAStoreID names the store within that server. One store per instance
	// serves every tenant (07.2). Empty until cmd/fga has created one.
	FGAStoreID string

	// FGAModelID pins the authorization model version. Empty means "whatever
	// is latest", which is right for development and wrong for production,
	// where 7.4 requires the model to be deployed before dependent code.
	FGAModelID string

	// HTTPAddr is the listen address for cmd/api.
	HTTPAddr string

	// AuthProvider names the Authenticator implementation to open (ADR-106,
	// D2). There is no default: a service that silently picked a provider
	// would be exactly the accident ADR-106's note requires be impossible.
	// "dev" is only ever satisfiable by a binary built with -tags dev — see
	// internal/auth.
	AuthProvider string

	// DevAuthSecret signs and verifies the dev provider's HS256 tokens
	// (D3). Read here unconditionally, but only REQUIRED when AuthProvider
	// is "dev" — that is a conditional requirement config.Load cannot
	// express with its flat missing-var check, so internal/auth/dev
	// enforces the minimum-length rule itself, at Authenticator
	// construction time, which is still before the process starts serving.
	DevAuthSecret string
}

// Load reads configuration from the environment. missing lists every absent
// required variable rather than the first, so that a fresh checkout learns
// everything it needs in one run.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:   os.Getenv("YMCA_DATABASE_URL"),
		FGAAPIURL:     os.Getenv("YMCA_FGA_API_URL"),
		FGAStoreID:    os.Getenv("YMCA_FGA_STORE_ID"),
		FGAModelID:    os.Getenv("YMCA_FGA_MODEL_ID"),
		HTTPAddr:      envOr("YMCA_HTTP_ADDR", ":8000"),
		AuthProvider:  os.Getenv("YMCA_AUTH_PROVIDER"),
		DevAuthSecret: os.Getenv("YMCA_DEV_AUTH_SECRET"),
	}

	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "YMCA_DATABASE_URL")
	}
	if c.FGAAPIURL == "" {
		missing = append(missing, "YMCA_FGA_API_URL")
	}
	if c.AuthProvider == "" {
		missing = append(missing, "YMCA_AUTH_PROVIDER")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config: required environment variables not set: %s",
			strings.Join(missing, ", "))
	}
	return c, nil
}

// LoadDatabaseOnly is for cmd/migrate, which touches PostgreSQL and nothing
// else. Requiring an OpenFGA address to run a migration would be a false
// dependency.
func LoadDatabaseOnly() (Config, error) {
	url := os.Getenv("YMCA_DATABASE_URL")
	if url == "" {
		return Config{}, fmt.Errorf("config: YMCA_DATABASE_URL is not set")
	}
	return Config{DatabaseURL: url}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
