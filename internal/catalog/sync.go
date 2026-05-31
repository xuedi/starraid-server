package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// execer is the subset of the pgx pool Sync needs; *pgxpool.Pool satisfies it.
// Keeping it minimal avoids a catalog→db import cycle (db imports catalog for the
// param schema on its load path).
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Sync upserts the code-defined catalog (classes, modules, items) into the
// catalog tables. It is idempotent — ON CONFLICT (key) DO UPDATE — so it re-syncs
// whenever the Go definitions change. Run on server startup after migrations
// (code is the single source of truth; goose owns the schema, this owns the rows).
func Sync(ctx context.Context, db execer) error {
	for _, c := range Classes {
		slots, err := json.Marshal(c.Slots)
		if err != nil {
			return fmt.Errorf("catalog: marshal slots for %q: %w", c.Key, err)
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO object_class (key, name, kind, size_class, base_mass, base_cargo_volume, slots)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (key) DO UPDATE SET
			   name = EXCLUDED.name, kind = EXCLUDED.kind, size_class = EXCLUDED.size_class,
			   base_mass = EXCLUDED.base_mass, base_cargo_volume = EXCLUDED.base_cargo_volume,
			   slots = EXCLUDED.slots`,
			c.Key, c.Name, c.Kind, c.SizeClass, c.BaseMass, c.BaseCargoVolume, slots,
		); err != nil {
			return fmt.Errorf("catalog: sync class %q: %w", c.Key, err)
		}
	}

	for _, m := range Modules {
		params, err := json.Marshal(m.Params)
		if err != nil {
			return fmt.Errorf("catalog: marshal params for %q: %w", m.Key, err)
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO module_types (key, name, slot_kind, size_class, mass, params)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (key) DO UPDATE SET
			   name = EXCLUDED.name, slot_kind = EXCLUDED.slot_kind, size_class = EXCLUDED.size_class,
			   mass = EXCLUDED.mass, params = EXCLUDED.params`,
			m.Key, m.Name, m.SlotKind, m.SizeClass, m.Mass, params,
		); err != nil {
			return fmt.Errorf("catalog: sync module %q: %w", m.Key, err)
		}
	}

	for _, it := range Items {
		if _, err := db.Exec(ctx,
			`INSERT INTO item_types (key, name, category, mass, volume)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (key) DO UPDATE SET
			   name = EXCLUDED.name, category = EXCLUDED.category,
			   mass = EXCLUDED.mass, volume = EXCLUDED.volume`,
			it.Key, it.Name, it.Category, it.Mass, it.Volume,
		); err != nil {
			return fmt.Errorf("catalog: sync item %q: %w", it.Key, err)
		}
	}
	return nil
}
