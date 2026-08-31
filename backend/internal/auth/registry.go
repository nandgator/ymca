package auth

import "fmt"

// registered holds every Authenticator implementation compiled into this
// binary, keyed by the name YMCA_AUTH_PROVIDER selects. An implementation
// adds itself from its own init() — see internal/auth/dev — so this file
// never imports a specific provider package.
//
// That indirection is load-bearing for D2's build gate. internal/auth/dev
// carries //go:build dev on every file. If this file imported it directly,
// a plain `go build ./...` would fail to compile at all whenever the dev
// package was absent, instead of simply lacking the "dev" provider — the
// opposite of ADR-106's requirement that a forgotten -tags dev produce a
// build WITHOUT the dev authenticator, never one WITH it.
var registered = map[string]func(Deps) (Authenticator, error){}

// Register makes an Authenticator implementation available under name.
// Call it from an implementation's init(), never from application code.
func Register(name string, open func(Deps) (Authenticator, error)) {
	if _, exists := registered[name]; exists {
		panic(fmt.Sprintf("auth: provider %q registered twice", name))
	}
	registered[name] = open
}

// Open constructs the Authenticator named by name. Unknown name is an
// error. D2's build gate is enforced by what got registered, not by logic
// here: a build without -tags dev never runs internal/auth/dev's init, so
// "dev" is simply absent from registered, and this reports that plainly
// rather than pretending the name was never valid.
func Open(name string, deps Deps) (Authenticator, error) {
	open, ok := registered[name]
	if !ok {
		if name == "dev" {
			return nil, fmt.Errorf(
				"auth: provider %q is not available: this build has no dev provider (compiled without -tags dev)", name)
		}
		return nil, fmt.Errorf("auth: unknown provider %q", name)
	}
	return open(deps)
}
