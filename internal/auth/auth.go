// Package auth authenticates connecting clients and bots. The path is identical
// for humans and NPCs — there is no privileged client (see docs/server.md,
// docs/protocol.md). DB-backed credentials are a later slice; this slice ships
// the interface plus a dev stub.
package auth

import (
	"context"
	"errors"
)

// ErrInvalidCredentials is returned when a username/secret pair is not accepted.
// Callers translate it into a LoginResult{ok:false}; they must not leak which of
// the two was wrong.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// Identity is the authenticated account the credentials resolve to. Per the
// domain model (docs/domain-model.md) login → account → character → object;
// choosing a character and assigning a controlled object (SelfAssign) is a later
// slice, so this carries only the account identity for now.
type Identity struct {
	AccountID string // stable account identifier (dev stub: the username)
	Username  string
}

// Authenticator validates credentials and resolves them to an Identity.
// Implementations return ErrInvalidCredentials for a bad pair, or another error
// for an infrastructure failure (e.g. the DB being unreachable).
type Authenticator interface {
	Authenticate(ctx context.Context, username, secret string) (Identity, error)
}
