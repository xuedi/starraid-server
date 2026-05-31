// Package game holds the authoritative in-memory world state and the tick loop.
package game

import (
	"context"
	"log/slog"
	"time"
)

// World is the authoritative, in-memory mirror of the *active slice* of the
// universe. PostgreSQL is the single source of truth; this holds only the
// objects in active areas (see docs/server.md, docs/database.md).
type World struct {
	// TODO: object store, active-area clusters, interest management,
	// cached per-object attributes (recomputed on module-config change).
}

// New creates an empty world. Real startup loads the world setting and
// active-area objects from the database.
func New() *World {
	return &World{}
}

// Tick advances the simulation by one step of duration dt. Pacing is tick-based
// with an adaptive per-area rate (see docs/server.md); this stub is fixed-rate.
func (w *World) Tick(dt time.Duration) {
	// TODO: integrate movement, resolve combat (size class + weapon triangle),
	// run power/economy over cached attributes, advance contracts.
}

// Run drives the tick loop until ctx is cancelled.
func (w *World) Run(ctx context.Context) {
	const interval = 100 * time.Millisecond // ~10 Hz placeholder; real rate is adaptive
	t := time.NewTicker(interval)
	defer t.Stop()
	slog.Info("world loop started", "interval", interval)

	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			slog.Info("world loop stopped")
			return
		case now := <-t.C:
			w.Tick(now.Sub(last))
			last = now
		}
	}
}
