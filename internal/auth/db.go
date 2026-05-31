package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/bcrypt"

	"github.com/xuedi/starraid-server/internal/db"
)

// DBAuthenticator authenticates against the PostgreSQL accounts table with
// bcrypt-hashed passwords, and resolves the controlled object the session will
// assign. It replaces the dev stub (see docs/server.md).
type DBAuthenticator struct {
	Pool *db.Pool
}

// Authenticate verifies email (the wire "username") + password against the
// accounts table. Unknown email, inactive account, and wrong password all return
// ErrInvalidCredentials without revealing which.
func (a DBAuthenticator) Authenticate(ctx context.Context, username, secret string) (Identity, error) {
	acc, err := a.Pool.AccountByEmail(ctx, username)
	if errors.Is(err, db.ErrNotFound) {
		return Identity{}, ErrInvalidCredentials
	}
	if err != nil {
		return Identity{}, fmt.Errorf("auth: account lookup: %w", err)
	}
	if acc.Status != "active" {
		return Identity{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(secret)); err != nil {
		return Identity{}, ErrInvalidCredentials
	}

	// Resolve the controlled object. Missing is not fatal here — the session
	// falls back to spawning one — so a fresh account can still log in.
	objID, err := a.Pool.ControlledObjectForAccount(ctx, acc.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return Identity{}, fmt.Errorf("auth: resolve controlled object: %w", err)
	}

	_ = a.Pool.UpdateAccountLastLogin(ctx, acc.ID) // best-effort

	return Identity{
		AccountID: strconv.FormatInt(acc.ID, 10),
		Username:  username,
		ObjectID:  objID,
	}, nil
}
