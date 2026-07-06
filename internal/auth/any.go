package auth

import (
	"context"
	"errors"
)

// Any tries each authenticator in order and returns the first Identity that
// validates. It lets one server accept several credential kinds at once — e.g.
// real DB accounts AND the dev stub — so dispatcher bots (dev creds, dev-spawned
// objects) coexist with human DB logins.
//
// A rejected pair falls through to the next authenticator. If none accept, it
// returns ErrInvalidCredentials — unless an authenticator hit an infrastructure
// error (e.g. the DB was unreachable), which is surfaced so real failures aren't
// masked as bad credentials.
type Any []Authenticator

func (a Any) Authenticate(ctx context.Context, username, secret string) (Identity, error) {
	var infraErr error
	for _, au := range a {
		id, err := au.Authenticate(ctx, username, secret)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, ErrInvalidCredentials) && infraErr == nil {
			infraErr = err
		}
	}
	if infraErr != nil {
		return Identity{}, infraErr
	}
	return Identity{}, ErrInvalidCredentials
}
