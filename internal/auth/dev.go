package auth

import (
	"context"
	"crypto/subtle"
)

// Dev is a stub Authenticator that accepts a single configured dev account.
// It exists so the handshake/login slice is exercisable without a database;
// the DB-backed Authenticator replaces it in a later slice. Do not ship it.
type Dev struct {
	User   string
	Secret string
}

// Authenticate accepts exactly the configured dev user/secret. Comparison is
// constant-time so the stub doesn't model a timing oracle. An empty configured
// secret rejects everything, so a misconfigured dev server can't accept-all.
func (d Dev) Authenticate(_ context.Context, username, secret string) (Identity, error) {
	if d.Secret == "" {
		return Identity{}, ErrInvalidCredentials
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(d.User)) == 1
	secretOK := subtle.ConstantTimeCompare([]byte(secret), []byte(d.Secret)) == 1
	if !userOK || !secretOK {
		return Identity{}, ErrInvalidCredentials
	}
	return Identity{AccountID: d.User, Username: d.User}, nil
}
