package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by the lookups below when no row matches. Callers
// translate it into a domain decision (e.g. invalid credentials) — it must not
// leak which of several inputs was wrong.
var ErrNotFound = errors.New("db: not found")

// Pool is the server's PostgreSQL connection pool plus the query methods the
// session/auth layers need. PostgreSQL is the single source of truth; this is
// the read path that loads the active slice into memory (see docs/database.md).
type Pool struct {
	*pgxpool.Pool
}

// Open creates a connection pool for dsn and verifies connectivity.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &Pool{p}, nil
}

// Account is the credential row used for login.
type Account struct {
	ID           int64
	PasswordHash string
	Status       string
}

// AccountByEmail looks up an account by its email (the wire "username").
func (p *Pool) AccountByEmail(ctx context.Context, email string) (Account, error) {
	var a Account
	err := p.QueryRow(ctx,
		`SELECT id, password_hash, status FROM accounts WHERE email = $1`, email,
	).Scan(&a.ID, &a.PasswordHash, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("db: account by email: %w", err)
	}
	return a, nil
}

// ControlledObjectForAccount resolves the object a player controls: the account's
// character's object (login → character → object). Returns ErrNotFound if the
// account has no character/object yet.
func (p *Pool) ControlledObjectForAccount(ctx context.Context, accountID int64) (uint64, error) {
	var id int64
	err := p.QueryRow(ctx,
		`SELECT o.id FROM objects o
		   JOIN characters c ON o.owner_character_id = c.id
		  WHERE c.account_id = $1
		  ORDER BY o.id
		  LIMIT 1`, accountID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("db: controlled object for account: %w", err)
	}
	return uint64(id), nil
}

// UpdateAccountLastLogin stamps the account's last_login_at. Best-effort.
func (p *Pool) UpdateAccountLastLogin(ctx context.Context, accountID int64) error {
	_, err := p.Exec(ctx, `UPDATE accounts SET last_login_at = now() WHERE id = $1`, accountID)
	return err
}

// SectorObject is a wire-relevant object row loaded for an active sector.
type SectorObject struct {
	ID   uint64
	X, Y int64
}

// FirstSectorID returns the id of the lowest-numbered sector (the starting area
// for now), and ok=false if no sector exists yet (DB not seeded).
func (p *Pool) FirstSectorID(ctx context.Context) (id int64, ok bool, err error) {
	err = p.QueryRow(ctx, `SELECT id FROM sectors ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("db: first sector: %w", err)
	}
	return id, true, nil
}

// LoadSectorObjects returns every object in the given sector (the naive active
// set for now; sector/interest scoping is refined in later slices).
func (p *Pool) LoadSectorObjects(ctx context.Context, sectorID int64) ([]SectorObject, error) {
	rows, err := p.Query(ctx,
		`SELECT id, x, y FROM objects WHERE sector_id = $1 ORDER BY id`, sectorID)
	if err != nil {
		return nil, fmt.Errorf("db: load sector objects: %w", err)
	}
	defer rows.Close()

	var out []SectorObject
	for rows.Next() {
		var o SectorObject
		var id int64
		if err := rows.Scan(&id, &o.X, &o.Y); err != nil {
			return nil, fmt.Errorf("db: scan object: %w", err)
		}
		o.ID = uint64(id)
		out = append(out, o)
	}
	return out, rows.Err()
}
