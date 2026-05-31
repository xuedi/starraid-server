package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xuedi/starraid-server/internal/catalog"
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

// SectorObject is an object loaded for an active sector, fat: its class base
// attributes plus its per-instance fitting (modules + cargo). The server derives
// the object's live attributes (mass/power/speed/shield) from this (see
// docs/objects.md); the catalog gives structure, the fitting gives configuration.
type SectorObject struct {
	ID       uint64
	X, Y     int64
	TypeKey  string // object_class.key — carried on the beacon for client sprites
	BaseMass int64  // object_class.base_mass
	Modules  []SectorModule
	Cargo    []SectorCargo
}

// SectorModule is one installed module on a loaded object: its mass plus the
// behaviour parameters the server's derivation reads.
type SectorModule struct {
	Mass   int64
	Params catalog.ModuleParams
}

// SectorCargo is one cargo stack on a loaded object: per-unit mass × quantity.
type SectorCargo struct {
	UnitMass int64
	Quantity int64
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
// set for now; sector/interest scoping is refined in later slices), each loaded
// fat: class base attributes joined from object_class, plus the per-instance
// fitting (object_modules joined to module_types, object_items joined to
// item_types). Three queries keyed by object_id rather than one wide join, so an
// object's modules and cargo don't multiply each other.
func (p *Pool) LoadSectorObjects(ctx context.Context, sectorID int64) ([]SectorObject, error) {
	rows, err := p.Query(ctx,
		`SELECT o.id, o.x, o.y, oc.key, oc.base_mass
		   FROM objects o JOIN object_class oc ON o.object_class_id = oc.id
		  WHERE o.sector_id = $1 ORDER BY o.id`, sectorID)
	if err != nil {
		return nil, fmt.Errorf("db: load sector objects: %w", err)
	}
	defer rows.Close()

	out := make([]SectorObject, 0)
	byID := make(map[uint64]*SectorObject)
	for rows.Next() {
		var o SectorObject
		var id int64
		if err := rows.Scan(&id, &o.X, &o.Y, &o.TypeKey, &o.BaseMass); err != nil {
			return nil, fmt.Errorf("db: scan object: %w", err)
		}
		o.ID = uint64(id)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: load sector objects: %w", err)
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	if err := p.loadSectorModules(ctx, sectorID, byID); err != nil {
		return nil, err
	}
	if err := p.loadSectorCargo(ctx, sectorID, byID); err != nil {
		return nil, err
	}
	return out, nil
}

// loadSectorModules attaches each object's installed modules (mass + behaviour
// params) to the matching SectorObject.
func (p *Pool) loadSectorModules(ctx context.Context, sectorID int64, byID map[uint64]*SectorObject) error {
	rows, err := p.Query(ctx,
		`SELECT om.object_id, mt.mass, mt.params
		   FROM object_modules om
		   JOIN module_types mt ON om.module_type_id = mt.id
		   JOIN objects o ON om.object_id = o.id
		  WHERE o.sector_id = $1`, sectorID)
	if err != nil {
		return fmt.Errorf("db: load sector modules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var objID int64
		var m SectorModule
		var params []byte
		if err := rows.Scan(&objID, &m.Mass, &params); err != nil {
			return fmt.Errorf("db: scan module: %w", err)
		}
		if err := json.Unmarshal(params, &m.Params); err != nil {
			return fmt.Errorf("db: module params for object %d: %w", objID, err)
		}
		if o := byID[uint64(objID)]; o != nil {
			o.Modules = append(o.Modules, m)
		}
	}
	return rows.Err()
}

// loadSectorCargo attaches each object's cargo stacks (per-unit mass × quantity)
// to the matching SectorObject.
func (p *Pool) loadSectorCargo(ctx context.Context, sectorID int64, byID map[uint64]*SectorObject) error {
	rows, err := p.Query(ctx,
		`SELECT oi.object_id, it.mass, oi.quantity
		   FROM object_items oi
		   JOIN item_types it ON oi.item_type_id = it.id
		   JOIN objects o ON oi.object_id = o.id
		  WHERE o.sector_id = $1`, sectorID)
	if err != nil {
		return fmt.Errorf("db: load sector cargo: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var objID int64
		var c SectorCargo
		if err := rows.Scan(&objID, &c.UnitMass, &c.Quantity); err != nil {
			return fmt.Errorf("db: scan cargo: %w", err)
		}
		if o := byID[uint64(objID)]; o != nil {
			o.Cargo = append(o.Cargo, c)
		}
	}
	return rows.Err()
}
